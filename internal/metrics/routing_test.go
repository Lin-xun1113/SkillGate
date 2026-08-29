package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAggregateTriggerRecallAndSpecificity(t *testing.T) {
	cases := []CaseMeta{
		{CaseID: "pos-1", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_trigger"},
		{CaseID: "pos-2", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_trigger"},
		{CaseID: "neg-1", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_not_trigger"},
		{CaseID: "neg-2", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_not_trigger"},
		{CaseID: "answer-1", EvaluationMode: "forced_injection", Population: "answer"},
		{CaseID: "probe-1", EvaluationMode: "security_probe", Population: "answer"},
	}
	trials := []TriggerInput{
		{CaseID: "pos-1", Arm: "with_skill", Passed: true, GradesJSON: []byte(`{"graders":[{"passed":true,"message":"skill_loaded"}]}`)},
		{CaseID: "pos-2", Arm: "with_skill", Passed: false},
		{CaseID: "neg-1", Arm: "with_skill", Passed: false},
		{CaseID: "neg-2", Arm: "with_skill", Passed: true, GradesJSON: []byte(`{"graders":[{"passed":true,"evidence":{"type":"skill_loaded"}}]}`)},
		{CaseID: "answer-1", Arm: "with_skill", Passed: true, GradesJSON: []byte(`{"graders":[{"passed":true,"message":"skill_loaded"}]}`)},
		{CaseID: "probe-1", Arm: "with_skill", Passed: true, GradesJSON: []byte(`{"graders":[{"passed":true,"message":"skill_loaded"}]}`)},
	}
	got := AggregateTrigger(cases, trials)
	if !got.Evaluated {
		t.Fatal("expected trigger metrics to be evaluated")
	}
	if got.Recall != 0.5 {
		t.Fatalf("recall = %v, want 0.5", got.Recall)
	}
	if got.Specificity != 0.5 {
		t.Fatalf("specificity = %v, want 0.5", got.Specificity)
	}
	if got.PositiveCases != 2 || got.NegativeCases != 2 {
		t.Fatalf("denominator leaked non-trigger cases: %+v", got)
	}
}

func TestAggregateTriggerMajorityTieIsNotLoaded(t *testing.T) {
	cases := []CaseMeta{
		{CaseID: "pos-1", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_trigger"},
		{CaseID: "neg-1", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_not_trigger"},
	}
	trials := []TriggerInput{
		{CaseID: "pos-1", Arm: "with_skill", RepetitionIndex: 0, Passed: true, GradesJSON: []byte(`{"graders":[{"passed":true,"message":"skill_loaded"}]}`)},
		{CaseID: "pos-1", Arm: "with_skill", RepetitionIndex: 1, Passed: false},
		{CaseID: "neg-1", Arm: "with_skill", Passed: false},
	}
	got := AggregateTrigger(cases, trials)
	if got.Recall != 0 {
		t.Fatalf("tie should count as not loaded, recall=%v", got.Recall)
	}
}

func TestAggregateTriggerReadsNestedSuiteSkillAssertion(t *testing.T) {
	cases := []CaseMeta{
		{CaseID: "positive", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_trigger"},
		{CaseID: "negative", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_not_trigger"},
	}
	grades := func(assertion string) []byte {
		value := map[string]any{"graders": []any{map[string]any{
			"passed": true,
			"evidence": map[string]any{"assertions": []any{map[string]any{
				"type": "trace_assertion", "message": assertion, "passed": true,
			}}},
		}}}
		data, _ := json.Marshal(value)
		return data
	}
	got := AggregateTrigger(cases, []TriggerInput{
		{CaseID: "positive", Arm: "with_skill", Passed: true, GradesJSON: grades("skill_loaded:csv-analysis")},
		{CaseID: "negative", Arm: "with_skill", Passed: true, GradesJSON: grades("skill_not_loaded:csv-analysis")},
	})
	if got.Recall != 1 || got.Specificity != 1 {
		t.Fatalf("nested skill assertions not reflected: %+v", got)
	}
}

func TestTrialSkillLoadedRequiresExplicitSkillAssertion(t *testing.T) {
	tests := []struct {
		name   string
		grades []byte
	}{
		{name: "empty", grades: nil},
		{name: "empty manifest", grades: []byte(`{}`)},
		{name: "malformed", grades: []byte(`{"graders":`)},
		{name: "generic pass", grades: []byte(`{"graders":[{"passed":true,"message":"answer quality passed","evidence":{"assertions":[{"id":"output-valid","passed":true}]} }]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if loaded := trialSkillLoaded(TriggerInput{Passed: true, GradesJSON: tt.grades}); loaded {
				t.Fatalf("generic trial pass must not imply skill loaded for %s", tt.name)
			}
		})
	}
}

func TestAggregateTriggerIncompleteWhenCandidateMissing(t *testing.T) {
	cases := []CaseMeta{
		{CaseID: "pos-1", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_trigger"},
		{CaseID: "neg-1", EvaluationMode: "autonomous_trigger", Population: "trigger", Polarity: "should_not_trigger"},
	}
	got := AggregateTrigger(cases, nil)
	if got.Evaluated {
		t.Fatal("missing trials must not evaluate trigger metrics")
	}
	if got.IncompleteCases != 2 {
		t.Fatalf("incomplete=%d, want 2", got.IncompleteCases)
	}
}

func TestAggregateSecurityCountsAndMissingEvidence(t *testing.T) {
	cases := []CaseMeta{
		{CaseID: "crit", EvaluationMode: "security_probe"},
		{CaseID: "high", EvaluationMode: "security_probe"},
		{CaseID: "confirmed", EvaluationMode: "security_probe"},
		{CaseID: "missing", EvaluationMode: "security_probe"},
		{CaseID: "answer", EvaluationMode: "forced_injection"},
	}
	findings := []SecurityFinding{
		{CaseID: "crit", Severity: "critical", Status: "observed", Present: true},
		{CaseID: "high", Severity: "high", Status: "fixture_observed", Present: true},
		{CaseID: "confirmed", Severity: "high", Status: "confirmed_exploit", Present: true},
	}
	got := AggregateSecurity(cases, findings)
	if got.Critical != 1 {
		t.Fatalf("critical=%d, want 1", got.Critical)
	}
	if got.High != 2 {
		t.Fatalf("high=%d, want 2", got.High)
	}
	if got.ConfirmedExploits != 1 {
		t.Fatalf("confirmed=%d, want 1", got.ConfirmedExploits)
	}
	if got.Evaluated {
		t.Fatal("missing scanner evidence must set evaluated=false")
	}
	if got.MissingEvidence != 1 {
		t.Fatalf("missing=%d, want 1", got.MissingEvidence)
	}
}

func TestLoadSecurityFindingEmptyObjectIsMissingEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-finding.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	finding, err := LoadSecurityFinding(path)
	if err != nil {
		t.Fatal(err)
	}
	if finding.Present {
		t.Fatalf("empty finding must not be present: %+v", finding)
	}
}

func TestFindingPathUsesSuiteOutputDirectory(t *testing.T) {
	got := FindingPath("/artifacts", "exp", "trial")
	want := filepath.Join("/artifacts", "exp", "trial", "output", "security-finding.json")
	if got != want {
		t.Fatalf("FindingPath = %q, want %q", got, want)
	}
}

func TestLoadSecurityFindingRequiresAuditableFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-finding.json")
	if err := os.WriteFile(path, []byte(`{"severity":"high"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	finding, err := LoadSecurityFinding(path)
	if err != nil {
		t.Fatal(err)
	}
	if finding.Present {
		t.Fatalf("finding without status must be incomplete: %+v", finding)
	}
}
