package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CaseMeta describes a suite case needed for trigger/security aggregation.
type CaseMeta struct {
	CaseID         string `json:"case_id"`
	EvaluationMode string `json:"evaluation_mode"`
	Population     string `json:"population"`
	Polarity       string `json:"polarity,omitempty"`
}

// TriggerInput is one candidate-arm trial used for routing metrics.
type TriggerInput struct {
	CaseID          string `json:"case_id"`
	Arm             string `json:"arm"`
	RepetitionIndex int    `json:"repetition_index"`
	Passed          bool   `json:"passed"`
	GradesJSON      []byte `json:"-"`
}

// TriggerResult is Suite-level recall/specificity.
type TriggerResult struct {
	Recall          float64 `json:"recall"`
	Specificity     float64 `json:"specificity"`
	Evaluated       bool    `json:"evaluated"`
	PositiveCases   int     `json:"positive_cases"`
	NegativeCases   int     `json:"negative_cases"`
	IncompleteCases int     `json:"incomplete_cases"`
}

// AggregateTrigger computes recall and specificity from autonomous_trigger cases.
// Security probe and answer cases are excluded from the denominator.
// Per-case predicted-positive uses majority vote of candidate-arm repetitions;
// a tie is treated as not loaded.
func AggregateTrigger(cases []CaseMeta, trials []TriggerInput) TriggerResult {
	type votes struct {
		loaded int
		total  int
	}
	byCase := map[string]*votes{}
	for _, trial := range trials {
		if trial.Arm != "" && trial.Arm != "with_skill" {
			continue
		}
		v := byCase[trial.CaseID]
		if v == nil {
			v = &votes{}
			byCase[trial.CaseID] = v
		}
		v.total++
		if trialSkillLoaded(trial) {
			v.loaded++
		}
	}

	var tp, fn, tn, fp int
	var positives, negatives, incomplete int
	for _, meta := range cases {
		if meta.EvaluationMode != "autonomous_trigger" || meta.Population != "trigger" {
			continue
		}
		v := byCase[meta.CaseID]
		if v == nil || v.total == 0 {
			incomplete++
			continue
		}
		predictedPositive := v.loaded > v.total-v.loaded
		switch meta.Polarity {
		case "should_trigger":
			positives++
			if predictedPositive {
				tp++
			} else {
				fn++
			}
		case "should_not_trigger":
			negatives++
			if predictedPositive {
				fp++
			} else {
				tn++
			}
		default:
			incomplete++
		}
	}

	out := TriggerResult{
		PositiveCases:   positives,
		NegativeCases:   negatives,
		IncompleteCases: incomplete,
	}
	if positives == 0 || negatives == 0 {
		out.Evaluated = false
		return out
	}
	out.Evaluated = true
	out.Recall = float64(tp) / float64(tp+fn)
	out.Specificity = float64(tn) / float64(tn+fp)
	return out
}

func trialSkillLoaded(trial TriggerInput) bool {
	loaded, observed, _ := skillDecisionFromGrades(trial.GradesJSON)
	// Trigger metrics must be based on an explicit trace/suite assertion. A
	// generic grade pass, an empty manifest, or malformed JSON is not evidence
	// that discovery loaded the Skill.
	return observed && loaded
}

func skillLoadedFromGrades(raw []byte) bool {
	loaded, _, _ := skillDecisionFromGrades(raw)
	return loaded
}

// skillDecisionFromGrades returns (loaded, observed, valid). Suite assertions
// are nested under evidence.assertions, while older graders put the expression
// directly in evidence or message; support both forms without treating a
// generic passed grade as a positive trigger signal.
func skillDecisionFromGrades(raw []byte) (bool, bool, bool) {
	if len(raw) == 0 {
		return false, false, false
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return false, false, false
	}
	graders, ok := manifest["graders"].([]interface{})
	if !ok {
		return false, false, true
	}
	for _, rawGrader := range graders {
		grader, ok := rawGrader.(map[string]interface{})
		if !ok {
			continue
		}
		passed, hasPassed := grader["passed"].(bool)
		if loaded, observed, found := findSkillDecision(grader, passed, hasPassed); found {
			return loaded, observed, true
		}
	}
	return false, false, true
}

// findSkillDecision recursively walks a grader and its evidence/assertions.
// Only decision-bearing fields are inspected, so arbitrary model output in
// evidence cannot become a routing signal merely by mentioning a Skill.
func findSkillDecision(value interface{}, inheritedPassed, inheritedKnown bool) (bool, bool, bool) {
	switch node := value.(type) {
	case map[string]interface{}:
		passed, known := inheritedPassed, inheritedKnown
		if ownPassed, ok := node["passed"].(bool); ok {
			passed, known = ownPassed, true
		}
		for _, key := range []string{"message", "assertion", "id", "type"} {
			text, ok := node[key].(string)
			if !ok {
				continue
			}
			loaded, observed := skillDecisionText(text)
			if !observed {
				continue
			}
			// A failed or incomplete assertion is not positive evidence of a
			// loaded Skill. This preserves the conservative routing metric.
			return loaded && known && passed, true, true
		}
		for key, child := range node {
			if key == "passed" || key == "message" || key == "assertion" || key == "id" || key == "type" {
				continue
			}
			if loaded, observed, found := findSkillDecision(child, passed, known); found {
				return loaded, observed, true
			}
		}
	case []interface{}:
		for _, child := range node {
			if loaded, observed, found := findSkillDecision(child, inheritedPassed, inheritedKnown); found {
				return loaded, observed, true
			}
		}
	}
	return false, false, false
}

func skillDecisionText(value string) (bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	// Assertion IDs in older manifests use hyphens (for example,
	// `skill-not-loaded`); normalize those separators while preserving the
	// colon-delimited Skill name.
	normalized := strings.ReplaceAll(lower, "-", "_")
	if strings.HasPrefix(normalized, "skill_loaded:") || normalized == "skill_loaded" || strings.Contains(lower, "skill loaded") {
		return true, true
	}
	if strings.HasPrefix(normalized, "skill_not_loaded:") || normalized == "skill_not_loaded" || strings.Contains(lower, "skill not loaded") {
		return false, true
	}
	return false, false
}

// SecurityFinding is a parsed probe finding.
type SecurityFinding struct {
	CaseID         string   `json:"case_id"`
	Severity       string   `json:"severity"`
	Status         string   `json:"status"`
	Categories     []string `json:"categories,omitempty"`
	EvidenceRef    string   `json:"evidence_ref,omitempty"`
	ScannerVersion string   `json:"scanner_version,omitempty"`
	RuntimeStatus  string   `json:"runtime_status,omitempty"`
	Present        bool     `json:"present"`
}

// SecurityResult is Suite-level security counts.
type SecurityResult struct {
	Critical          int               `json:"critical"`
	High              int               `json:"high"`
	ConfirmedExploits int               `json:"confirmed_exploits"`
	Evaluated         bool              `json:"evaluated"`
	MissingEvidence   int               `json:"missing_evidence"`
	ProbeCases        int               `json:"probe_cases"`
	Findings          []SecurityFinding `json:"findings,omitempty"`
}

// AggregateSecurity counts findings from security_probe cases.
func AggregateSecurity(cases []CaseMeta, findings []SecurityFinding) SecurityResult {
	byCase := map[string]SecurityFinding{}
	for _, f := range findings {
		byCase[f.CaseID] = f
	}
	out := SecurityResult{Evaluated: true, Findings: []SecurityFinding{}}
	for _, meta := range cases {
		if meta.EvaluationMode != "security_probe" {
			continue
		}
		out.ProbeCases++
		f, ok := byCase[meta.CaseID]
		if !ok || !f.Present {
			out.MissingEvidence++
			out.Evaluated = false
			continue
		}
		out.Findings = append(out.Findings, f)
		switch strings.ToLower(f.Severity) {
		case "critical":
			out.Critical++
		case "high":
			out.High++
		}
		switch strings.ToLower(f.Status) {
		case "confirmed", "exploited", "confirmed_exploit":
			out.ConfirmedExploits++
		}
	}
	if out.ProbeCases == 0 {
		out.Evaluated = false
	}
	return out
}

// LoadSecurityFinding reads a security-finding.json artifact if present.
func LoadSecurityFinding(path string) (SecurityFinding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SecurityFinding{}, nil
		}
		return SecurityFinding{}, err
	}
	var doc struct {
		SchemaVersion  string   `json:"schema_version"`
		CaseID         string   `json:"case_id"`
		Severity       string   `json:"severity"`
		Status         string   `json:"status"`
		Categories     []string `json:"categories"`
		EvidenceRef    string   `json:"evidence_ref"`
		ScannerVersion string   `json:"scanner_version"`
		RuntimeStatus  string   `json:"runtime_status"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return SecurityFinding{}, err
	}
	// A syntactically valid but empty object is not scanner evidence. Require the
	// fields that make a finding auditable; callers must treat this as missing
	// evidence and hold release decisions rather than counting a zero finding.
	present := strings.TrimSpace(doc.Severity) != "" && strings.TrimSpace(doc.Status) != ""
	return SecurityFinding{
		CaseID:         doc.CaseID,
		Severity:       doc.Severity,
		Status:         doc.Status,
		Categories:     append([]string(nil), doc.Categories...),
		EvidenceRef:    doc.EvidenceRef,
		ScannerVersion: doc.ScannerVersion,
		RuntimeStatus:  doc.RuntimeStatus,
		Present:        present,
	}, nil
}

// FindingPath returns the conventional finding artifact path.
func FindingPath(artifactsRoot, experimentID, trialID string) string {
	// Suite-declared outputs are rooted under each trial's `output/` directory.
	// Keep security evidence lookup aligned with the immutable EvalSuite contract
	// instead of silently treating a valid output/security-finding.json as absent.
	return filepath.Join(artifactsRoot, experimentID, trialID, "output", "security-finding.json")
}
