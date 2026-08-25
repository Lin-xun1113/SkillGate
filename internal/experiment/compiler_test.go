package experiment

import "testing"

func TestUnsupportedTreatmentDiagnostic(t *testing.T) {
	diagnostic := ValidateTreatment("old_skill")
	if diagnostic.Code != "UNSUPPORTED_TREATMENT" {
		t.Fatalf("code = %s", diagnostic.Code)
	}
}

func TestCompilePairUsesTwoArms(t *testing.T) {
	plan, err := CompilePairPlan(CompileInput{
		ManifestHash:    "sha256:manifest",
		SuiteHash:       "sha256:suite",
		CaseID:          "case-1",
		EvaluationMode:  "forced_injection",
		Repetition:      1,
		ModelHash:       "sha256:model",
		HarnessHash:     "sha256:harness",
		EnvironmentHash: "sha256:env",
		FixtureHash:     "sha256:fixture",
		GraderHash:      "sha256:grader",
		ToolPolicyHash:  "sha256:tools",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Trials) != 2 || plan.Trials[0].Arm != "without_skill" || plan.Trials[1].Arm != "with_skill" {
		t.Fatalf("unexpected trials: %#v", plan.Trials)
	}
}
