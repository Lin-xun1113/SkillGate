package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ManifestPair represents a single test pair in the manifest
type ManifestPair struct {
	ID         string                 `json:"id"`
	Treatment  map[string]interface{} `json:"treatment"`
	Control    map[string]interface{} `json:"control,omitempty"`
	Fixtures   []interface{}          `json:"fixtures,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
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
	manifestPath := filepath.Join(casRoot, "manifests", manifestHash[:2], manifestHash+".json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Verify hash
	computed := sha256.Sum256(data)
	computedHash := hex.EncodeToString(computed[:])
	if computedHash != manifestHash {
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

// ComputeExecutionHash computes the deterministic execution hash
// from manifest_hash, case_id, arm, skill_hash, model, tool_policy, and environment
func ComputeExecutionHash(
	manifestHash string,
	caseID string,
	arm string,
	skillHash string,
	model map[string]interface{},
	toolPolicy map[string]interface{},
	environment map[string]interface{},
) (string, error) {
	// Build canonical structure
	exec := map[string]interface{}{
		"manifest_hash": manifestHash,
		"case_id":       caseID,
		"arm":           arm,
		"skill_hash":    skillHash,
		"model":         canonicalize(model),
		"tool_policy":   canonicalize(toolPolicy),
		"environment":   canonicalize(environment),
	}

	// Serialize with sorted keys
	data, err := json.Marshal(exec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal execution spec: %w", err)
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// canonicalize recursively sorts map keys for deterministic JSON output
func canonicalize(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		result := make(map[string]interface{}, len(val))
		for _, k := range keys {
			result[k] = canonicalize(val[k])
		}
		return result

	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = canonicalize(item)
		}
		return result

	default:
		return val
	}
}
