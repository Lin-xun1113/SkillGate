package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Projector resolves manifest content and projects execution specs into trial payloads.
type Projector struct {
	projectRoot string
}

// NewProjector creates a projector for the given project root.
func NewProjector(projectRoot string) *Projector {
	return &Projector{projectRoot: projectRoot}
}

// Project implements ExecutionProjector interface.
func (p *Projector) Project(ctx context.Context, manifestHash, pairID, arm string) (*ExecutionSpec, string, error) {
	// Resolve manifest path from hash via CAS
	manifestPath := filepath.Join(p.projectRoot, ".skillgate", "cas", manifestHash[:2], manifestHash, "manifest.yaml")
	return p.ProjectForClaim(manifestPath, pairID, arm)
}

// ProjectForClaim resolves the manifest, extracts the case and strategy, and returns ExecutionSpec + hash.
func (p *Projector) ProjectForClaim(manifestPath, pairID, arm string) (*ExecutionSpec, string, error) {
	compiled, diagnostics := manifest.Compile(manifestPath, p.projectRoot)
	if len(diagnostics) > 0 {
		return nil, "", fmt.Errorf("manifest compilation failed: %v", diagnostics[0].Message)
	}

	// Find the pair plan matching pairID
	var targetPair *struct {
		CaseID   string
		Strategy string
	}
	for _, pair := range compiled.Pairs {
		if pair.PairID == pairID {
			// Map arm to strategy
			strategy := ""
			if arm == "without_skill" {
				strategy = "baseline"
			} else if arm == "with_skill" {
				strategy = "candidate"
			}
			targetPair = &struct {
				CaseID   string
				Strategy string
			}{
				CaseID:   pair.CaseID,
				Strategy: strategy,
			}
			break
		}
	}

	if targetPair == nil {
		return nil, "", fmt.Errorf("pair_id %s not found in manifest", pairID)
	}

	// Load manifest YAML to extract strategies and suite
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifestDoc map[string]any
	if err := yaml.Unmarshal(manifestData, &manifestDoc); err != nil {
		return nil, "", fmt.Errorf("failed to parse manifest: %w", err)
	}

	spec, _ := manifestDoc["spec"].(map[string]any)

	// Extract strategies
	strategies := make(map[string]map[string]any)
	if strategyList, ok := spec["strategies"].([]any); ok {
		for _, raw := range strategyList {
			if item, ok := raw.(map[string]any); ok {
				if name, ok := item["name"].(string); ok {
					strategies[name] = item
				}
			}
		}
	}

	strategyConfig, ok := strategies[targetPair.Strategy]
	if !ok {
		return nil, "", fmt.Errorf("strategy %s not found", targetPair.Strategy)
	}

	// Extract skill hash (empty for baseline)
	skillHash := ""
	if skills, ok := strategyConfig["skills"].([]any); ok && len(skills) > 0 {
		if skillMap, ok := skills[0].(map[string]any); ok {
			skillHash, _ = skillMap["hash"].(string)
		}
	}

	// Extract model
	model, _ := strategyConfig["model"].(map[string]any)

	// Extract tool policy
	toolPolicy := map[string]any{
		"tools":   strategyConfig["tools"],
		"budget":  strategyConfig["budget"],
		"retry":   strategyConfig["retry"],
		"sandbox": strategyConfig["sandbox"],
	}

	// Extract environment
	execution, _ := spec["execution"].(map[string]any)
	environment := map[string]any{}
	if envRef, ok := execution["environmentDescriptor"].(string); ok {
		envPath := filepath.Join(filepath.Dir(manifestPath), envRef)
		envData, err := os.ReadFile(envPath)
		if err == nil {
			var envDoc map[string]any
			if yaml.Unmarshal(envData, &envDoc) == nil {
				if envSpec, ok := envDoc["spec"].(map[string]any); ok {
					environment = envSpec
				}
			}
		}
	}

	// Load suite to extract case input (fixtures)
	suiteRef, _ := spec["suite"].(string)
	suitePath := filepath.Join(filepath.Dir(manifestPath), suiteRef)
	suiteData, err := os.ReadFile(suitePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read suite: %w", err)
	}

	var suiteDoc map[string]any
	if err := yaml.Unmarshal(suiteData, &suiteDoc); err != nil {
		return nil, "", fmt.Errorf("failed to parse suite: %w", err)
	}

	suiteSpec, _ := suiteDoc["spec"].(map[string]any)
	cases, _ := suiteSpec["cases"].([]any)

	var caseInput map[string]any
	for _, raw := range cases {
		if caseMap, ok := raw.(map[string]any); ok {
			if id, _ := caseMap["id"].(string); id == targetPair.CaseID {
				fixtures, _ := caseMap["fixtures"].([]any)
				fixturesList := make([]map[string]any, 0, len(fixtures))
				for _, f := range fixtures {
					if fm, ok := f.(map[string]any); ok {
						fixturesList = append(fixturesList, fm)
					}
				}
				caseInput = map[string]any{
					"id":             id,
					"evaluationMode": caseMap["evaluationMode"],
					"population":     caseMap["population"],
					"fixtures":       fixturesList,
				}
				break
			}
		}
	}

	if caseInput == nil {
		return nil, "", fmt.Errorf("case_id %s not found in suite", targetPair.CaseID)
	}

	executionSpec := &ExecutionSpec{
		CaseID:      targetPair.CaseID,
		CaseInput:   caseInput,
		SkillHash:   skillHash,
		Model:       model,
		ToolPolicy:  toolPolicy,
		Environment: environment,
	}

	// Compute execution_hash
	executionHash, err := identity.HashCanonical(executionSpec)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compute execution hash: %w", err)
	}

	return executionSpec, executionHash, nil
}
