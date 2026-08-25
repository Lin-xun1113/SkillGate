package experiment

import "testing"

func TestPairCompatibilityRejectsEveryNonTreatmentMutation(t *testing.T) {
	base := PairCompatibility{ModelHash: "model", PromptHash: "prompt", FixtureHash: "fixture", HarnessHash: "harness", EnvironmentHash: "env", ToolPolicyHash: "tools", BudgetHash: "budget", RetryHash: "retry", GraderHash: "grader", Repetition: 1}
	mutations := []struct {
		name   string
		mutate func(*PairCompatibility)
	}{
		{"model", func(v *PairCompatibility) { v.ModelHash = "other" }},
		{"prompt", func(v *PairCompatibility) { v.PromptHash = "other" }},
		{"fixture", func(v *PairCompatibility) { v.FixtureHash = "other" }},
		{"harness", func(v *PairCompatibility) { v.HarnessHash = "other" }},
		{"environment", func(v *PairCompatibility) { v.EnvironmentHash = "other" }},
		{"tools", func(v *PairCompatibility) { v.ToolPolicyHash = "other" }},
		{"budget", func(v *PairCompatibility) { v.BudgetHash = "other" }},
		{"retry", func(v *PairCompatibility) { v.RetryHash = "other" }},
		{"grader", func(v *PairCompatibility) { v.GraderHash = "other" }},
		{"repetition", func(v *PairCompatibility) { v.Repetition = 2 }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := base
			mutation.mutate(&candidate)
			diagnostic := ValidatePairCompatibility(base, candidate)
			if diagnostic.Code != "PAIR_IDENTITY_MISMATCH" {
				t.Fatalf("code=%q diagnostic=%#v", diagnostic.Code, diagnostic)
			}
		})
	}
}
