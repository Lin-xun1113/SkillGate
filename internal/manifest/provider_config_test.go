package manifest

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateProviderModelAcceptsCanonicalAndLegacyOptions(t *testing.T) {
	model := map[string]any{
		"provider": "openai",
		"name":     "gpt-test",
		"config": map[string]any{
			"temperature":     0.25,
			"max_tokens":      1024,
			"timeout_seconds": 30,
			"max_retries":     2,
		},
	}
	if diagnostics := validateProviderModel(model, "spec.strategies[0].model"); len(diagnostics) != 0 {
		t.Fatalf("canonical model rejected: %#v", diagnostics)
	}

	legacy := map[string]any{
		"type":     "anthropic",
		"model_id": "claude-test",
		"params": map[string]any{
			"temperature": 0,
			"maxTokens":   1024,
			"timeout":     1.5,
			"maxRetries":  0,
		},
	}
	if diagnostics := validateProviderModel(legacy, "model"); len(diagnostics) != 0 {
		t.Fatalf("legacy model rejected: %#v", diagnostics)
	}
}

func TestValidateProviderModelRejectsUnknownAndInvalidOptions(t *testing.T) {
	cases := []struct {
		name  string
		model map[string]any
		want  string
	}{
		{
			name:  "unknown option",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"top_p": 0.5}},
			want:  "top_p",
		},
		{
			name:  "string number",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"temperature": "0"}},
			want:  "temperature",
		},
		{
			name:  "temperature lower bound",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"temperature": -0.1}},
			want:  "temperature",
		},
		{
			name:  "temperature upper bound",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"temperature": 2.1}},
			want:  "temperature",
		},
		{
			name:  "token float",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"max_tokens": 1.0}},
			want:  "max_tokens",
		},
		{
			name:  "token upper bound",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"max_tokens": providerMaxTokens + 1}},
			want:  "max_tokens",
		},
		{
			name:  "timeout upper bound",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"timeout_seconds": providerMaxTimeoutSeconds + 1}},
			want:  "timeout_seconds",
		},
		{
			name:  "retry upper bound",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"max_retries": providerMaxRetries + 1}},
			want:  "max_retries",
		},
		{
			name:  "duplicate alias",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"max_tokens": 1, "maxTokens": 1}},
			want:  "maxTokens",
		},
		{
			name:  "runtime secret",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"headers": map[string]any{"Authorization": "x"}}},
			want:  "headers",
		},
		{
			name:  "nonfinite",
			model: map[string]any{"provider": "openai", "name": "m", "config": map[string]any{"temperature": math.Inf(1)}},
			want:  "temperature",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics := validateProviderModel(tc.model, "model")
			if len(diagnostics) == 0 {
				t.Fatalf("expected diagnostics")
			}
			found := false
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Path, tc.want) || strings.Contains(diagnostic.Message, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %q in diagnostics=%#v", tc.want, diagnostics)
			}
		})
	}
}

func TestCompileRejectsInvalidProviderConfig(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	source, err := os.ReadFile(filepath.Join(root, "experiments/csv-analysis-v1-demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		from string
		to   string
		path string
	}{
		{"unknown", "          temperature: 0", "          temperature: 0\n          top_p: 0.5", "top_p"},
		{"string-number", "          temperature: 0", "          temperature: \"0\"", "temperature"},
		{"too-many-tokens", "          temperature: 0", "          temperature: 0\n          max_tokens: 1000001", "max_tokens"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			mutated := strings.Replace(string(source), mutation.from, mutation.to, 1)
			path := filepath.Join(root, "experiments", ".provider-config-"+mutation.name+".yaml")
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			_, diagnostics := Compile(path, root)
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Path, mutation.path) || strings.Contains(diagnostic.Message, mutation.path) {
					return
				}
			}
			t.Fatalf("expected provider config diagnostic for %s: %#v", mutation.path, diagnostics)
		})
	}
}
