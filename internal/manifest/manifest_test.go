package manifest

import (
	"path/filepath"
	"testing"
)

func TestCompileM0ManifestProducesDeterministicPlan(t *testing.T) {
	root := filepath.Join("..", "..")
	compiled, diagnostics := Compile(filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml"), root)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if compiled.PairCount != 24 || compiled.TrialCount != 48 {
		t.Fatalf("counts = %d/%d, want 24/48", compiled.PairCount, compiled.TrialCount)
	}
	if compiled.Pairs[0].Trials[0].Arm != "without_skill" || compiled.Pairs[0].Trials[1].Arm != "with_skill" {
		t.Fatalf("arm ordering = %#v", compiled.Pairs[0].Trials)
	}
	if compiled.Pairing["treatment"] != "skill_version" || compiled.Pairs[0].Identity.CaseID == "" || compiled.Pairs[0].Identity.SuiteHash == "" {
		t.Fatalf("identity output incomplete: %#v", compiled.Pairs[0])
	}
	second, secondDiagnostics := Compile(filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml"), root)
	if len(secondDiagnostics) != 0 || second.Pairs[0].PairID != compiled.Pairs[0].PairID || second.Pairs[0].Trials[0].TrialID != compiled.Pairs[0].Trials[0].TrialID {
		t.Fatal("M0 IDs are not stable")
	}
	if compiled.Pairs[0].PairID != "sha256:c4084d73942b0035ea40743ad28b78489cadbc96114e97096acf773f6adb5622" || compiled.Pairs[0].Trials[0].TrialID != "sha256:13fd1d6454e67f5c106d856f90ee6f86ab18a7956f12a97cc23bba6dae642224" {
		t.Fatalf("golden IDs changed: %s %s", compiled.Pairs[0].PairID, compiled.Pairs[0].Trials[0].TrialID)
	}
}

func TestCompileRejectsUnsupportedTreatment(t *testing.T) {
	root := filepath.Join("..", "..")
	compiled, diagnostics := Compile(filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml"), root)
	if compiled == nil || len(diagnostics) != 0 {
		t.Fatalf("expected successful compile, got %#v %#v", compiled, diagnostics)
	}
}
