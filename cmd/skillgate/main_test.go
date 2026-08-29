package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/Lin-xun1113/SkillGate/internal/secrets"
)

func TestExitCodeContract(t *testing.T) {
	cases := map[string]int{
		"INVALID_ARGUMENT":       2,
		"FILE_NOT_FOUND":         3,
		"PATH_OUTSIDE_ROOT":      4,
		"SECRET_FIELD_PRESENT":   4,
		"INVALID_SKILL_MANIFEST": 5,
		"PAIR_IDENTITY_MISMATCH": 6,
		"LEAKAGE_DETECTED":       6,
		"CONTENT_HASH_MISMATCH":  6,
		"REGISTRY_CONFLICT":      7,
		"IO_ERROR":               8,
	}
	for code, want := range cases {
		if got := codeFor(code); got != want {
			t.Errorf("codeFor(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestJSONOutputHasVersionedDocument(t *testing.T) {
	value := map[string]any{"version": "skillgate.cli.v1", "diagnostics": []any{}}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["version"] != "skillgate.cli.v1" {
		t.Fatalf("version = %#v", decoded["version"])
	}
	if got := codeFor("REGISTRY_CONFLICT"); got != 7 {
		t.Fatalf("registry conflict code = %d", got)
	}
}

func TestDatabaseURLUsesFileSourceWithoutInlineSecret(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "database-url")
	if err := os.WriteFile(path, []byte("postgres://skillgate:sentinel-db-password@localhost/db?sslmode=disable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := databaseURL([]string{"--database-url-file", path})
	if err != nil {
		t.Fatalf("databaseURL() error: %v", err)
	}
	if value == "" || value[len(value)-1] == '\n' {
		t.Fatalf("databaseURL() returned untrimmed value %q", value)
	}
}

func TestDatabaseURLRejectsPasswordInCommandLine(t *testing.T) {
	_, err := databaseURL([]string{"--database-url", "postgres://skillgate:sentinel-db-password@localhost/db"})
	var schedulerErr *scheduler.Error
	if !errors.As(err, &schedulerErr) || schedulerErr.Code != scheduler.CodeInvalidArgument {
		t.Fatalf("databaseURL() error = %T %v, want INVALID_ARGUMENT", err, err)
	}
	if stringsContains(err.Error(), "sentinel-db-password") {
		t.Fatalf("error leaked password: %v", err)
	}
}

func TestDatabaseURLMissingSecretHasStableCode(t *testing.T) {
	for _, name := range []string{"SKILLGATE_DATABASE_URL", "SKILLGATE_DATABASE_URL_FILE", "DATABASE_URL", "DATABASE_URL_FILE"} {
		old, existed := os.LookupEnv(name)
		_ = os.Unsetenv(name)
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(name, old)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	_, err := databaseURL(nil)
	var schedulerErr *scheduler.Error
	if !errors.As(err, &schedulerErr) || schedulerErr.Code != scheduler.CodeSecretMissing {
		t.Fatalf("databaseURL() error = %T %v, want SECRET_MISSING", err, err)
	}
	var secretErr *secrets.Error
	if errors.As(err, &secretErr) {
		t.Fatalf("databaseURL should expose scheduler boundary, got raw secret error: %v", err)
	}
}

func stringsContains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
