package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func runCLI(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	root := filepath.Join("..", "..")
	binary := filepath.Join(t.TempDir(), "skillgate")
	build := exec.Command("go", "build", "-o", binary, "./cmd/skillgate")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	return cmd.Output()
}

func TestCompileCLIJSONFailureIsVersionedAndNonZero(t *testing.T) {
	output, err := runCLI(t, "compile", "does-not-exist.yaml", "--json")
	if err == nil {
		t.Fatal("expected CLI failure")
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatalf("stdout is not JSON: %v: %s", err, output)
	}
	if document["version"] != "skillgate.cli.v1" || document["ok"] != false {
		t.Fatalf("envelope = %#v", document)
	}
	if _, ok := document["diagnostics"]; !ok {
		t.Fatalf("diagnostics missing: %#v", document)
	}
}

func TestCLIUsageAndMissingPathExitCodes(t *testing.T) {
	if _, err := runCLI(t, "unknown", "--json"); err == nil {
		t.Fatal("unknown command should fail")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("unknown command err=%v", err)
	}
	if _, err := runCLI(t, "skill", "validate", "/tmp/skillgate-no-such-path", "--json"); err == nil {
		t.Fatal("missing skill should fail")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 3 {
		t.Fatalf("missing skill err=%v", err)
	}
}
