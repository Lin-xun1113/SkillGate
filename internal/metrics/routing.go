package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CaseMeta describes a suite case needed for trigger/security aggregation.
type CaseMeta struct {
	CaseID         string
	EvaluationMode string
	Population     string
	Polarity       string
}

// TriggerInput is one candidate-arm trial used for routing metrics.
type TriggerInput struct {
	CaseID          string
	Arm             string
	RepetitionIndex int
	Passed          bool
	GradesJSON      []byte
}

// TriggerResult is Suite-level recall/specificity.
type TriggerResult struct {
	Recall          float64
	Specificity     float64
	Evaluated       bool
	PositiveCases   int
	NegativeCases   int
	IncompleteCases int
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
	if skillLoadedFromGrades(trial.GradesJSON) {
		return true
	}
	return trial.Passed
}

func skillLoadedFromGrades(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var manifest struct {
		Graders []struct {
			Passed   bool                   `json:"passed"`
			Message  string                 `json:"message"`
			Evidence map[string]interface{} `json:"evidence"`
		} `json:"graders"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return false
	}
	for _, g := range manifest.Graders {
		if !g.Passed {
			continue
		}
		blob := strings.ToLower(g.Message)
		if strings.Contains(blob, "skill_loaded") || strings.Contains(blob, "skill loaded") {
			return true
		}
		if ev, ok := g.Evidence["assertion"].(string); ok && strings.Contains(strings.ToLower(ev), "skill_loaded") {
			return true
		}
		if ev, ok := g.Evidence["type"].(string); ok && ev == "skill_loaded" {
			return true
		}
	}
	return false
}

// SecurityFinding is a parsed probe finding.
type SecurityFinding struct {
	CaseID   string
	Severity string
	Status   string
	Present  bool
}

// SecurityResult is Suite-level security counts.
type SecurityResult struct {
	Critical          int
	High              int
	ConfirmedExploits int
	Evaluated         bool
	MissingEvidence   int
	ProbeCases        int
}

// AggregateSecurity counts findings from security_probe cases.
func AggregateSecurity(cases []CaseMeta, findings []SecurityFinding) SecurityResult {
	byCase := map[string]SecurityFinding{}
	for _, f := range findings {
		byCase[f.CaseID] = f
	}
	out := SecurityResult{Evaluated: true}
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
		CaseID   string `json:"case_id"`
		Severity string `json:"severity"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return SecurityFinding{}, err
	}
	return SecurityFinding{
		CaseID:   doc.CaseID,
		Severity: doc.Severity,
		Status:   doc.Status,
		Present:  true,
	}, nil
}

// FindingPath returns the conventional finding artifact path.
func FindingPath(artifactsRoot, experimentID, trialID string) string {
	return filepath.Join(artifactsRoot, experimentID, trialID, "security-finding.json")
}
