package secrets

import (
	"errors"
	"testing"
)

func TestLookupWithPrefersFileAndTrimsNewline(t *testing.T) {
	value, err := LookupWith("TEST_TOKEN", func(name string) (string, bool) {
		switch name {
		case "TEST_TOKEN_FILE":
			return "/run/secrets/token", true
		case "TEST_TOKEN":
			return "direct-value", true
		default:
			return "", false
		}
	}, func(path string) ([]byte, error) {
		if path != "/run/secrets/token" {
			t.Fatalf("unexpected path %q", path)
		}
		return []byte("file-value\n"), nil
	})
	if err != nil || value != "file-value" {
		t.Fatalf("LookupWith() = %q, %v", value, err)
	}
}

func TestLookupWithStableTypedFailures(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		code Code
	}{
		{name: "missing", env: map[string]string{}, code: CodeMissing},
		{name: "empty", env: map[string]string{"TOKEN": "  "}, code: CodeEmpty},
		{name: "empty file path", env: map[string]string{"TOKEN_FILE": "  "}, code: CodeEmpty},
		{name: "unreadable", env: map[string]string{"TOKEN_FILE": "/missing"}, code: CodeUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LookupWith("TOKEN", func(name string) (string, bool) {
				value, ok := tc.env[name]
				return value, ok
			}, func(string) ([]byte, error) {
				return nil, errors.New("synthetic read failure containing sentinel")
			})
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("error = %T %v, want code %s", err, err, tc.code)
			}
			if typed.Code == CodeUnreadable && containsSecret(err.Error()) {
				t.Fatal("error leaked read failure details")
			}
		})
	}
}

func TestLookupWithRejectsInvalidName(t *testing.T) {
	_, err := LookupWith("../TOKEN", nil, nil)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != CodeInvalidName {
		t.Fatalf("error = %v, want invalid-name error", err)
	}
}

func containsSecret(value string) bool { return value == "sentinel" }
