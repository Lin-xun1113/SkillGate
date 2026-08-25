package validation

import "testing"

func TestWorkspacePathRejectsExpectedAndAbsolutePaths(t *testing.T) {
	cases := map[string]string{"../out.json": "PATH_OUTSIDE_ROOT", "/tmp/out.json": "PATH_OUTSIDE_ROOT", "output/expected/result.json": "LEAKAGE_DETECTED", "output/answer-key.json": "LEAKAGE_DETECTED"}
	for path, want := range cases {
		if got := ValidateWorkspacePath(path); got != want {
			t.Errorf("ValidateWorkspacePath(%q)=%q want %q", path, got, want)
		}
	}
}
