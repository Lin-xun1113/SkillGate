package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
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
		Environment: map[string]any{}, // populated from manifest execution section if needed
	}

	// Compute execution_hash
	execHash, err := computeExecutionHash(spec)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compute execution hash: %w", err)
	}

	return spec, execHash, nil
}

// extractMap safely extracts a nested map.
func extractMap(parent map[string]any, key string) map[string]any {
	if val, ok := parent[key].(map[string]any); ok {
		return val
	}
	return map[string]any{}
}

// computeExecutionHash produces canonical SHA-256 of ExecutionSpec.
func computeExecutionHash(spec *runner.ExecutionSpec) (string, error) {
	bytes, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	// Canonical JSON: sorted keys
	var canonical map[string]interface{}
	if err := json.Unmarshal(bytes, &canonical); err != nil {
		return "", err
	}
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonicalBytes)
	return hex.EncodeToString(hash[:]), nil
}
