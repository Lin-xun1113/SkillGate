package metrics

import "testing"

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
