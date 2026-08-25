package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalJSONSortsKeysAndNormalizesNestedValues(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{
		"z": "line1\r\nline2",
		"a": map[string]any{"b": 2, "a": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"a":1,"b":2},"z":"line1\nline2"}`
	if string(got) != want {
		t.Fatalf("canonical JSON = %q, want %q", got, want)
	}
}

func TestSkillPackageHashIsStableAcrossLineEndingsAndDirectoryOrder(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, root := range []string{first, second} {
		if err := os.Mkdir(filepath.Join(root, "references"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, "SKILL.md"), []byte("---\nname: csv-analysis\r\ndescription: test\r\n---\r\nbody\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "references", "guide.txt"), []byte("hello\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "references", "guide.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "SKILL.md"), []byte("---\nname: csv-analysis\ndescription: test\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	left, err := SkillPackageHash(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := SkillPackageHash(second)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("hashes differ: %s vs %s", left, right)
	}
}

func TestSkillPackageRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: test\ndescription: test\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := SkillPackageHash(root); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}

func TestPairAndTrialIDsAreDeterministic(t *testing.T) {
	input := PairInput{ExperimentHash: "sha256:e", SuiteHash: "sha256:s", CaseID: "case-1", EvaluationMode: "forced_injection", Repetition: 1, Treatment: "skill_version", BaselineArm: "without_skill", CandidateArm: "with_skill", ModelHash: "sha256:m", HarnessHash: "sha256:h", EnvironmentHash: "sha256:env", FixtureHash: "sha256:f", GraderHash: "sha256:g", ToolPolicyHash: "sha256:t"}
	pair, err := PairID(input)
	if err != nil {
		t.Fatal(err)
	}
	trial, err := TrialID(pair, "without_skill", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair) != len("sha256:")+64 || len(trial) != len("sha256:")+64 {
		t.Fatalf("unexpected IDs: %s %s", pair, trial)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(`{"a":1}`), &decoded); err != nil {
		t.Fatal(err)
	}
}
