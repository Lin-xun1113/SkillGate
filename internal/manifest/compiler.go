package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	// Prefer a compiled manifest in CAS, but support source-only development and
	// Compose deployments by deterministically finding and compiling the source.
	var compiled CompiledExperiment
	var compiledLoaded bool
	for _, compiledPath := range c.compiledManifestCandidates(manifestHash) {
		compiledData, readErr := os.ReadFile(compiledPath)
		if readErr != nil {
			continue
		}
		if err := json.Unmarshal(compiledData, &compiled); err != nil {
			return nil, fmt.Errorf("failed to parse compiled manifest: %w", err)
		}
		compiledLoaded = true
		break
	}

	manifestPath, err := c.findManifestByHash(manifestHash)
	if err != nil {
		return nil, fmt.Errorf("original manifest not found: %w", err)
	}
	if !compiledLoaded {
		compiledResult, diagnostics := Compile(manifestPath, c.projectRoot)
		if len(diagnostics) > 0 || compiledResult == nil {
			if len(diagnostics) > 0 {
				return nil, fmt.Errorf("manifest compilation failed: %s", diagnostics[0].Message)
			}
			return nil, fmt.Errorf("manifest compilation returned no result")
		}
		compiled = *compiledResult
	}
	if compiled.ManifestHash != "" && !sameHash(compiled.ManifestHash, manifestHash) {
		return nil, fmt.Errorf("compiled manifest hash mismatch: expected %s, got %s", manifestHash, compiled.ManifestHash)
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
		Hash:        manifestHash,
		Compiled:    &compiled,
		Raw:         raw,
		SourcePath:  manifestPath,
		projectRoot: c.projectRoot,
	}, nil
}

// findManifestByHash searches for manifest file matching the hash.
func (c *Compiler) findManifestByHash(hash string) (string, error) {
	experimentsDir := filepath.Join(c.projectRoot, "experiments")
	var found string
	err := filepath.WalkDir(experimentsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if found != "" {
			return filepath.SkipAll
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil
		}
		compiled, diagnostics := Compile(path, c.projectRoot)
		if len(diagnostics) == 0 && compiled != nil && sameHash(compiled.ManifestHash, hash) {
			found = path
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	if found != "" {
		return found, nil
	}
	// Keep a useful development fallback for a single manifest outside the
	// conventional experiments directory.
	for _, root := range []string{c.projectRoot, filepath.Join(c.projectRoot, "experiments")} {
		for _, name := range []string{"main.yaml", "manifest.yaml"} {
			path := filepath.Join(root, name)
			if _, statErr := os.Stat(path); statErr == nil {
				compiled, diagnostics := Compile(path, c.projectRoot)
				if len(diagnostics) == 0 && compiled != nil && sameHash(compiled.ManifestHash, hash) {
					return path, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no manifest found for hash %s", hash)
}

func (c *Compiler) compiledManifestCandidates(hash string) []string {
	digest := strings.TrimPrefix(hash, "sha256:")
	if len(digest) < 2 {
		return nil
	}
	base := filepath.Join(c.casRoot, "manifests")
	return []string{
		filepath.Join(base, hash+".json"),
		filepath.Join(base, digest+".json"),
		filepath.Join(base, digest[:2], hash+".json"),
		filepath.Join(base, digest[:2], digest+".json"),
	}
}

func sameHash(left, right string) bool {
	return left == right || strings.TrimPrefix(left, "sha256:") == strings.TrimPrefix(right, "sha256:")
}

// LoadedManifest provides access to both compiled and raw manifest data.
type LoadedManifest struct {
	Hash        string
	Compiled    *CompiledExperiment
	Raw         map[string]any
	SourcePath  string
	projectRoot string
}

// ProjectRoot returns the root used to resolve manifest references. It is
// exposed to execution projectors so external descriptors can be resolved
// consistently with the compiler's path rules.
func (m *LoadedManifest) ProjectRoot() string {
	if m == nil {
		return ""
	}
	return m.projectRoot
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

	// Resolve the external suite from the source manifest. The raw manifest's
	// `spec.suite` field is a path string, not an embedded object.
	spec, _ := m.Raw["spec"].(map[string]any)
	suiteRef, _ := spec["suite"].(string)
	suitePath := filepath.Join(filepath.Dir(m.SourcePath), suiteRef)
	if _, err := os.Stat(suitePath); err != nil {
		suitePath = filepath.Join(m.projectRoot, suiteRef)
	}
	suiteData, err := os.ReadFile(suitePath)
	if err != nil {
		return nil, fmt.Errorf("suite %s could not be read: %w", suiteRef, err)
	}
	var suiteDoc map[string]any
	if err := yaml.Unmarshal(suiteData, &suiteDoc); err != nil {
		return nil, fmt.Errorf("suite %s could not be parsed: %w", suiteRef, err)
	}
	suiteSpec, _ := suiteDoc["spec"].(map[string]any)
	cases, _ := suiteSpec["cases"].([]any)

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

	// Extract strategies (the manifest schema uses an array).
	strategies := map[string]map[string]any{}
	if list, ok := spec["strategies"].([]any); ok {
		for _, raw := range list {
			if item, ok := raw.(map[string]any); ok {
				if name, ok := item["name"].(string); ok {
					strategies[name] = item
				}
			}
		}
	}
	baseline := strategies["baseline"]
	candidate := strategies["candidate"]

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
