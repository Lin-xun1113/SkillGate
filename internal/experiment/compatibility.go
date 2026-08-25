package experiment

import "github.com/Lin-xun1113/SkillGate/internal/validation"

type PairCompatibility struct {
	ModelHash       string
	PromptHash      string
	FixtureHash     string
	HarnessHash     string
	EnvironmentHash string
	ToolPolicyHash  string
	BudgetHash      string
	RetryHash       string
	GraderHash      string
	Repetition      int
}

func ValidatePairCompatibility(baseline, candidate PairCompatibility) validation.Diagnostic {
	if baseline != candidate {
		return validation.Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "pair.identity", Message: "Baseline 与 Candidate 的非 Treatment Identity 不一致。"}
	}
	return validation.Diagnostic{}
}
