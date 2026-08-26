package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Lin-xun1113/SkillGate/internal/experiment"
	"gopkg.in/yaml.v3"
)

// Compiler loads manifests and resolves execution specs.
type Compiler struct {
	projectRoot string
	casRoot     string
}

// NewCompiler creates a manifest compiler.
func NewCompiler(projectRoot, casRoot string) *Compiler {
	return &Compiler{
		projectRoot: projectRoot,
		casRoot:     casRoot,
	}
}

// Load reads both compiled JSON and original YAML to provide full context.
func (c *Compiler) Load(ctx context.Context, manifestHash string) (*LoadedManifest, error) {
	// Load compiled manifest from CAS
	compiledPath := filepath.Join(c.casRoot, "manifests", manifestHash+".json")
	compiledData, err := os.ReadFile(compiledPath)
	if err != nil {
		return nil, fmt.Errorf("manifest not found in CAS: %w", err)
	}

	var compiled CompiledExperiment
	if err := json.Unmarshal(compiledData, &compiled); err != nil {
		return nil, fmt.Errorf("failed to parse compiled manifest: %w", err)
	}

	// Load original YAML manifest to get case inputs and strategy configs
	// Search for manifest file by hash in experiments/
	manifestPath, err := c.findManifestByHash(manifestHash)
	if err != nil {
		return nil, fmt.Errorf("original manifest not found: %w", err)
	}

	rawData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest YAML: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(rawData, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	return &LoadedManifest{
		Hash:     manifestHash,
		Compiled: &compiled,
		Raw:      raw,
	}, nil
}

// findManifestByHash searches for manifest file matching the hash.
func (c *Compiler) findManifestByHash(hash string) (string, error) {
	// For M4, we assume manifest is at experiments/<hash>.yaml or similar
	// In production, this should check a manifest registry or CAS metadata
	experimentsDir := filepath.Join(c.projectRoot, "experiments")

	// Try direct hash match
	candidates := []string{
		filepath.Join(experimentsDir, hash+".yaml"),
		filepath.Join(experimentsDir, "main.yaml"), // fallback for development
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no manifest found for hash %s", hash)
}

// LoadedManifest provides access to both compiled and raw manifest data.
type LoadedManifest struct {
	Hash     string
	Compiled *CompiledExperiment
	Raw      map[string]any
}

// GetPair retrieves pair by ID with full execution context.
func (m *LoadedManifest) GetPair(pairID string) (*PairContext, error) {
	// Find in compiled pairs
	var plan *experiment.PairPlan
	for i := range m.Compiled.Pairs {
		if m.Compiled.Pairs[i].PairID == pairID {
			plan = &m.Compiled.Pairs[i]
			break
		}
	}
	if plan == nil {
		return nil, fmt.Errorf("pair %s not found", pairID)
	}

	// Extract case input from raw manifest
	spec, _ := m.Raw["spec"].(map[string]any)
	suite, _ := spec["suite"].(map[string]any)
	cases, _ := suite["cases"].([]any)

	var caseInput map[string]any
	for _, c := range cases {
		caseMap, _ := c.(map[string]any)
		if id, _ := caseMap["id"].(string); id == plan.CaseID {
			caseInput = caseMap
			break
		}
	}
	if caseInput == nil {
		return nil, fmt.Errorf("case %s not found in manifest", plan.CaseID)
	}

	// Extract strategies
	strategies, _ := spec["strategies"].(map[string]any)
	baseline, _ := strategies["baseline"].(map[string]any)
	candidate, _ := strategies["candidate"].(map[string]any)

	return &PairContext{
		Plan:      plan,
		CaseInput: caseInput,
		Baseline:  baseline,
		Candidate: candidate,
	}, nil
}

// PairContext contains all context needed to build ExecutionSpec.
type PairContext struct {
	Plan      *experiment.PairPlan
	CaseInput map[string]any
	Baseline  map[string]any
	Candidate map[string]any
}
