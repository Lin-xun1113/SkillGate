package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
)

// ManifestPair represents a single test pair in the manifest
type ManifestPair struct {
	ID        string                 `json:"id"`
	Treatment map[string]interface{} `json:"treatment"`
	Control   map[string]interface{} `json:"control,omitempty"`
	Fixtures  []interface{}          `json:"fixtures,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Manifest represents the test manifest structure
type Manifest struct {
	SchemaVersion string                 `json:"schema_version"`
	SkillHash     string                 `json:"skill_hash"`
	Pairs         []ManifestPair         `json:"pairs"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// LoadManifest loads and validates a manifest file from CAS
func LoadManifest(casRoot, manifestHash string) (*Manifest, error) {
	digest := strings.TrimPrefix(manifestHash, "sha256:")
	if len(digest) < 2 {
		return nil, fmt.Errorf("invalid manifest hash: %q", manifestHash)
	}
	candidates := []string{
		filepath.Join(casRoot, "manifests", digest[:2], manifestHash+".json"),
		filepath.Join(casRoot, "manifests", digest[:2], digest+".json"),
		filepath.Join(casRoot, "manifests", manifestHash+".json"),
		filepath.Join(casRoot, "manifests", digest+".json"),
	}
	var data []byte
	var err error
	for _, path := range candidates {
		data, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Verify both the current prefixed identity and legacy bare hashes.
	computedHash := identity.HashBytes(data)
	if computedHash != manifestHash && strings.TrimPrefix(computedHash, "sha256:") != digest {
		return nil, fmt.Errorf("manifest hash mismatch: expected %s, got %s", manifestHash, computedHash)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// ResolveCase finds a test case by ID in the manifest
func (m *Manifest) ResolveCase(caseID string) (*ManifestPair, error) {
	for _, pair := range m.Pairs {
		if pair.ID == caseID {
			return &pair, nil
		}
	}
	return nil, fmt.Errorf("case %s not found in manifest", caseID)
}

// ComputeExecutionHash computes the legacy execution hash input. New callers
// should use ComputeExecutionHashForSpec so the exact payload sent to a worker
// is hashed (including case_input).
func ComputeExecutionHash(
	manifestHash string,
	caseID string,
	arm string,
	skillHash string,
	model map[string]interface{},
	toolPolicy map[string]interface{},
	environment map[string]interface{},
) (string, error) {
	// Keep the old signature source-compatible while delegating to the one
	// canonical algorithm. manifestHash and arm remain outer request identity.
	return ComputeExecutionHashForSpec(&ExecutionSpec{
		CaseID: caseID, CaseInput: map[string]any{}, SkillHash: skillHash,
		Model: model, ToolPolicy: toolPolicy, Environment: environment,
	})
}

// ComputeExecutionHashForSpec hashes exactly the execution object in the
// TrialRequest payload. identity.HashCanonical provides sorted keys, compact
// JSON, newline normalization and the sha256: prefix shared by Python.
func ComputeExecutionHashForSpec(spec *ExecutionSpec) (string, error) {
	if spec == nil {
		return "", fmt.Errorf("execution spec is nil")
	}
	// Do not hash the Go struct directly: encoding/json preserves struct field
	// declaration order, while the Python worker receives a JSON object and
	// canonicalizes its keys lexicographically. Hash an explicit map so both
	// runtimes use the same canonical bytes.
	return identity.HashCanonical(map[string]any{
		"case_id":     spec.CaseID,
		"case_input":  spec.CaseInput,
		"skill_hash":  spec.SkillHash,
		"model":       spec.Model,
		"tool_policy": spec.ToolPolicy,
		"environment": spec.Environment,
	})
}
