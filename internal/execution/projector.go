package execution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"gopkg.in/yaml.v3"
)

// Projector resolves manifest_hash + pair_id + arm into ExecutionSpec.
type Projector struct {
	compiler *manifest.Compiler
}

// NewProjector creates an execution content projector.
func NewProjector(compiler *manifest.Compiler) *Projector {
	return &Projector{compiler: compiler}
}

// Project resolves execution content from manifest_hash + pair_id + arm.
func (p *Projector) Project(ctx context.Context, manifestHash, pairID, arm string) (*runner.ExecutionSpec, string, error) {
	// Load manifest
	mf, err := p.compiler.Load(ctx, manifestHash)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load manifest %s: %w", manifestHash, err)
	}

	// Get pair context
	pairCtx, err := mf.GetPair(pairID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get pair %s: %w", pairID, err)
	}

	// Select strategy by arm
	var strategy map[string]any
	var skillHash string
	switch arm {
	case "with_skill":
		strategy = pairCtx.Candidate
		// Extract skill_hash from candidate.skills[0].version
		if skills, ok := strategy["skills"].([]any); ok && len(skills) > 0 {
			if skill, ok := skills[0].(map[string]any); ok {
				skillHash, _ = skill["version"].(string)
			}
		}
	case "without_skill":
		strategy = pairCtx.Baseline
		skillHash = "" // baseline has no skill
	default:
		return nil, "", fmt.Errorf("invalid arm: %s", arm)
	}

	// Build ExecutionSpec
	spec := &runner.ExecutionSpec{
		CaseID:    pairCtx.Plan.CaseID,
		CaseInput: pairCtx.CaseInput,
		SkillHash: skillHash,
		Model:     extractMap(strategy, "model"),
		ToolPolicy: map[string]any{
			"tools":   extractMap(strategy, "tools"),
			"budget":  extractMap(strategy, "budget"),
			"retry":   extractMap(strategy, "retry"),
			"sandbox": extractMap(strategy, "sandbox"),
		},
		Environment: environmentFromManifest(mf),
	}

	// Hash exactly the execution object sent in TrialRequest. The runner package
	// owns this canonical algorithm so Go and Python cannot drift.
	execHash, err := runner.ComputeExecutionHashForSpec(spec)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compute execution hash: %w", err)
	}

	return spec, execHash, nil
}

func environmentFromManifest(mf *manifest.LoadedManifest) map[string]any {
	if mf == nil {
		return map[string]any{}
	}
	rawSpec, _ := mf.Raw["spec"].(map[string]any)
	execution, _ := rawSpec["execution"].(map[string]any)
	ref, _ := execution["environmentDescriptor"].(string)
	if ref == "" {
		return map[string]any{}
	}
	candidates := []string{
		filepath.Join(filepath.Dir(mf.SourcePath), ref),
		filepath.Join(mf.ProjectRoot(), ref),
		ref,
		filepath.Join("environments", filepath.Base(ref)),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc map[string]any
		if yaml.Unmarshal(data, &doc) != nil {
			continue
		}
		if value, ok := doc["spec"].(map[string]any); ok {
			return value
		}
		return doc
	}
	return map[string]any{}
}

// extractMap safely extracts a nested map.
func extractMap(parent map[string]any, key string) map[string]any {
	if val, ok := parent[key].(map[string]any); ok {
		return val
	}
	return map[string]any{}
}
