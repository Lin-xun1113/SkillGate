package validation

import "testing"

func TestDiagnosticJSONIsStable(t *testing.T) {
	got := Diagnostic{Code: "PAIR_IDENTITY_MISMATCH", Severity: "error", Path: "spec.strategies.candidate.model", Message: "Baseline 与 Candidate 的非 Treatment Identity 不一致。"}
	if got.ExitCode() != 6 {
		t.Fatalf("exit code = %d", got.ExitCode())
	}
	if got.Code != "PAIR_IDENTITY_MISMATCH" {
		t.Fatalf("code = %s", got.Code)
	}
}
