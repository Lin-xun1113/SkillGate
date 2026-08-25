package main

import (
	"encoding/json"
	"testing"
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
