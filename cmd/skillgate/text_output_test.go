package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCLIHumanCompileIsSummaryNotJSON(t *testing.T) {
	root := filepath.Join("..", "..")
	binary := filepath.Join(t.TempDir(), "skillgate")
	build := exec.Command("go", "build", "-o", binary, "./cmd/skillgate")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	cmd := exec.Command(binary, "compile", "experiments/csv-analysis-v1-demo.yaml")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(out), []byte("{")) {
		t.Fatalf("human output looks like JSON: %s", out)
	}
	if !bytes.Contains(out, []byte("pairs=24")) {
		t.Fatalf("summary missing count: %s", out)
	}
}
