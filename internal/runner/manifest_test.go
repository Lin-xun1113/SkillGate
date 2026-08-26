package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	// Create temporary CAS structure
	tmpDir := t.TempDir()
	casRoot := filepath.Join(tmpDir, "cas")
	manifestsDir := filepath.Join(casRoot, "manifests")

	manifest := Manifest{
		SchemaVersion: "1.0",
		SkillHash:     "skill123",
		Pairs: []ManifestPair{
			{
				ID: "case1",
				Treatment: map[string]interface{}{
					"skill_hash": "skill123",
				},
				Fixtures: []interface{}{},
			},
		},
	}

	data, _ := json.Marshal(manifest)
	hash := sha256.Sum256(data)
	manifestHash := hex.EncodeToString(hash[:])

	// Write manifest to CAS
	prefix := manifestHash[:2]
	os.MkdirAll(filepath.Join(manifestsDir, prefix), 0755)
	manifestPath := filepath.Join(manifestsDir, prefix, manifestHash+".json")
	os.WriteFile(manifestPath, data, 0644)

	// Test load
	loaded, err := LoadManifest(casRoot, manifestHash)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	if loaded.SkillHash != "skill123" {
		t.Errorf("skill_hash = %s, want skill123", loaded.SkillHash)
	}

	if len(loaded.Pairs) != 1 {
		t.Errorf("len(pairs) = %d, want 1", len(loaded.Pairs))
	}
}

func TestLoadManifestHashMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	casRoot := filepath.Join(tmpDir, "cas")
	manifestsDir := filepath.Join(casRoot, "manifests", "ab")
	os.MkdirAll(manifestsDir, 0755)

	// Write corrupted content
	wrongHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	manifestPath := filepath.Join(manifestsDir, wrongHash+".json")
	os.WriteFile(manifestPath, []byte(`{"corrupted": true}`), 0644)

	_, err := LoadManifest(casRoot, wrongHash)
	if err == nil {
		t.Fatal("Expected hash mismatch error, got nil")
	}
}

func TestResolveCase(t *testing.T) {
	manifest := &Manifest{
		Pairs: []ManifestPair{
			{ID: "case1", Treatment: map[string]interface{}{"x": 1}},
			{ID: "case2", Treatment: map[string]interface{}{"x": 2}},
		},
	}

	pair, err := manifest.ResolveCase("case1")
	if err != nil {
		t.Fatalf("ResolveCase failed: %v", err)
	}
	if pair.ID != "case1" {
		t.Errorf("Resolved wrong case: %s", pair.ID)
	}

	_, err = manifest.ResolveCase("case99")
	if err == nil {
		t.Error("Expected error for missing case, got nil")
	}
}

func TestComputeExecutionHash(t *testing.T) {
	hash1, err := ComputeExecutionHash(
		"manifest123",
		"case1",
		"treatment",
		"skill_abc123",
		map[string]interface{}{"provider": "mock"},
		map[string]interface{}{},
		map[string]interface{}{},
	)
	if err != nil {
		t.Fatalf("ComputeExecutionHash failed: %v", err)
	}

	// Same inputs should produce same hash
	hash2, _ := ComputeExecutionHash(
		"manifest123",
		"case1",
		"treatment",
		"skill_abc123",
		map[string]interface{}{"provider": "mock"},
		map[string]interface{}{},
		map[string]interface{}{},
	)

	if hash1 != hash2 {
		t.Errorf("Hashes differ for same input: %s != %s", hash1, hash2)
	}

	// Different case should produce different hash
	hash3, _ := ComputeExecutionHash(
		"manifest123",
		"case2", // Different
		"treatment",
		"skill_abc123",
		map[string]interface{}{"provider": "mock"},
		map[string]interface{}{},
		map[string]interface{}{},
	)

	if hash1 == hash3 {
		t.Error("Hashes should differ for different case_id")
	}
}

func TestComputeExecutionHashCanonicalization(t *testing.T) {
	// Different key order should produce same hash
	hash1, _ := ComputeExecutionHash(
		"manifest123",
		"case1",
		"treatment",
		"skill_abc123",
		map[string]interface{}{"a": 1, "b": 2, "c": 3},
		map[string]interface{}{},
		map[string]interface{}{},
	)

	hash2, _ := ComputeExecutionHash(
		"manifest123",
		"case1",
		"treatment",
		"skill_abc123",
		map[string]interface{}{"c": 3, "a": 1, "b": 2}, // Different order
		map[string]interface{}{},
		map[string]interface{}{},
	)

	if hash1 != hash2 {
		t.Errorf("Canonicalization failed: %s != %s", hash1, hash2)
	}
}
