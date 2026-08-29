package grader

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"gopkg.in/yaml.v3"
)

// Registry manages grader configurations
type Registry struct {
	mu      sync.RWMutex
	graders map[string]*Grader // key: grader hash
	rootDir string
}

// NewRegistry creates a new grader registry
func NewRegistry(rootDir string) *Registry {
	return &Registry{
		graders: make(map[string]*Grader),
		rootDir: rootDir,
	}
}

// Register registers a grader from a file path and returns its hash
func (r *Registry) Register(graderPath string) (string, error) {
	// Read grader file
	data, err := os.ReadFile(graderPath)
	if err != nil {
		return "", fmt.Errorf("failed to read grader file: %w", err)
	}

	// Parse as generic YAML first to check kind and calculate hash
	var genericDoc map[string]any
	if err := yaml.Unmarshal(data, &genericDoc); err != nil {
		return "", fmt.Errorf("failed to parse grader YAML: %w", err)
	}

	kind, _ := genericDoc["kind"].(string)

	// Handle DeterministicGraderSet by expanding it into a Grader
	if kind == "DeterministicGraderSet" {
		grader, err := r.expandDeterministicGraderSet(genericDoc, graderPath)
		if err != nil {
			return "", fmt.Errorf("failed to expand DeterministicGraderSet: %w", err)
		}

		// Validate expanded grader
		if err := r.validateGrader(grader); err != nil {
			return "", fmt.Errorf("invalid grader: %w", err)
		}

		// Calculate hash using canonical JSON (same as manifest compiler)
		hash, err := r.calculateHash(genericDoc)
		if err != nil {
			return "", fmt.Errorf("failed to calculate hash: %w", err)
		}

		// Store in registry
		r.mu.Lock()
		r.graders[hash] = grader
		r.mu.Unlock()

		return hash, nil
	}

	// Parse as standard Grader
	var grader Grader
	if err := yaml.Unmarshal(data, &grader); err != nil {
		return "", fmt.Errorf("failed to parse grader YAML: %w", err)
	}
	resolveGraderConfigPaths(&grader, filepath.Dir(graderPath))

	// Validate grader
	if err := r.validateGrader(&grader); err != nil {
		return "", fmt.Errorf("invalid grader: %w", err)
	}

	// Calculate hash using canonical JSON (same as manifest compiler)
	hash, err := r.calculateHash(genericDoc)
	if err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Store in registry
	r.mu.Lock()
	r.graders[hash] = &grader
	r.mu.Unlock()

	return hash, nil
}

func resolveGraderConfigPaths(grader *Grader, baseDir string) {
	if grader == nil || grader.Spec.Config == nil {
		return
	}
	for _, key := range []string{"schema_path", "expected_file", "expected_path", "rubric_path"} {
		if value, ok := grader.Spec.Config[key].(string); ok && value != "" {
			grader.Spec.Config[key] = resolveRelativePath(baseDir, value)
		}
	}
}

// expandDeterministicGraderSet converts a DeterministicGraderSet into a Grader
func (r *Registry) expandDeterministicGraderSet(doc map[string]any, graderPath string) (*Grader, error) {
	metadata, _ := doc["metadata"].(map[string]any)
	spec, _ := doc["spec"].(map[string]any)

	name, _ := metadata["name"].(string)
	version := 1
	if v, ok := metadata["version"].(int); ok {
		version = v
	} else if v, ok := metadata["version"].(float64); ok {
		version = int(v)
	}

	mode, _ := spec["mode"].(string)
	llmJudge, _ := spec["llmJudge"].(bool)

	// Build Grader struct
	grader := &Grader{
		APIVersion: "skillgate.dev/v1alpha1",
		Kind:       "Grader",
		Metadata: GraderMetadata{
			Name:    name,
			Version: version,
		},
		Spec: GraderSpec{
			Type: GraderTypeDeterministic,
			// Keep the historical JSONSchema method for compatibility with existing
			// manifests. The executor detects suite_cases in Config and evaluates
			// the complete assertion set instead of silently validating one file.
			Method:  MethodJSONSchema,
			Config:  map[string]interface{}{},
			Scoring: ScoringConfig{PassedScore: 1, FailedScore: 0},
		},
	}

	// Copy optional fields
	if mode != "" {
		grader.Spec.Mode = mode
	}
	grader.Spec.LLMJudge = llmJudge

	// Resolve relative references to absolute paths
	graderDir := filepath.Dir(graderPath)

	if suiteRef, ok := spec["suiteRef"].(string); ok {
		grader.Spec.SuiteRef = resolveRelativePath(graderDir, suiteRef)
		// Materialize the suite assertions into the immutable grader config. A
		// DeterministicGraderSet is a declarative bundle; keeping the resolved
		// assertion data here makes execution independent of the caller's cwd.
		if suiteData, err := os.ReadFile(grader.Spec.SuiteRef); err == nil {
			var suiteDoc map[string]any
			if err := yaml.Unmarshal(suiteData, &suiteDoc); err == nil {
				if suiteSpec, ok := suiteDoc["spec"].(map[string]any); ok {
					if cases, ok := suiteSpec["cases"].([]any); ok {
						grader.Spec.Config["suite_cases"] = normalizeSuiteCases(cases, filepath.Dir(grader.Spec.SuiteRef))
					}
				}
			}
		}
	}

	if semantics, ok := spec["assertionSemantics"].(string); ok {
		grader.Spec.AssertionSemantics = semantics
	}

	if schemaRefs, ok := spec["schemaRefs"].([]any); ok {
		for _, ref := range schemaRefs {
			if refStr, ok := ref.(string); ok {
				grader.Spec.SchemaRefs = append(grader.Spec.SchemaRefs, resolveRelativePath(graderDir, refStr))
			}
		}
	}

	if expectedRefs, ok := spec["expectedRefs"].([]any); ok {
		for _, ref := range expectedRefs {
			if refStr, ok := ref.(string); ok {
				grader.Spec.ExpectedRefs = append(grader.Spec.ExpectedRefs, resolveRelativePath(graderDir, refStr))
			}
		}
	}

	if boundary, ok := spec["boundary"].(map[string]any); ok {
		grader.Spec.Boundary = boundary
	}
	if grader.Spec.SuiteRef != "" {
		grader.Spec.Config["suite_path"] = grader.Spec.SuiteRef
	}
	if len(grader.Spec.SchemaRefs) > 0 {
		grader.Spec.Config["schema_refs"] = append([]string(nil), grader.Spec.SchemaRefs...)
	}
	if len(grader.Spec.ExpectedRefs) > 0 {
		grader.Spec.Config["expected_refs"] = append([]string(nil), grader.Spec.ExpectedRefs...)
	}

	return grader, nil
}

// normalizeSuiteCases resolves assertion references relative to the suite and
// converts yaml.v3's generic values into the map[string]interface{} shape used
// by the executor. The original suite document remains the source of the
// content hash; these values are only an execution projection.
func normalizeSuiteCases(cases []any, suiteDir string) []any {
	out := make([]any, 0, len(cases))
	for _, raw := range cases {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		copyItem := make(map[string]any, len(item))
		for key, value := range item {
			copyItem[key] = normalizeSuiteValue(value, suiteDir, key)
		}
		out = append(out, copyItem)
	}
	return out
}

func normalizeSuiteValue(value any, baseDir, parentKey string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if key == "schemaRef" || key == "expectedRef" || key == "findingRef" || key == "traceRef" || key == "evidenceRef" {
				if ref, ok := child.(string); ok {
					out[key] = resolveRelativePath(baseDir, ref)
					continue
				}
			}
			out[key] = normalizeSuiteValue(child, baseDir, key)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeSuiteValue(child, baseDir, parentKey)
		}
		return out
	default:
		return value
	}
}

// resolveRelativePath resolves a relative path from a base directory
func resolveRelativePath(base, ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(base, ref)
}

// Get retrieves a grader by its hash
func (r *Registry) Get(graderHash string) (*Grader, error) {
	r.mu.RLock()
	grader, ok := r.graders[graderHash]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("grader not found: %s", graderHash)
	}

	return grader, nil
}

// Validate validates a grader hash exists
func (r *Registry) Validate(graderHash string) error {
	r.mu.RLock()
	_, ok := r.graders[graderHash]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("grader not found: %s", graderHash)
	}

	return nil
}

// LoadFromDirectory loads grader definitions from a directory tree.
//
// Evaluation bundles commonly keep a DeterministicGraderSet next to its
// EvalSuite and referenced schema/expected files. Only Grader and
// DeterministicGraderSet documents are registry entries; other YAML documents
// are ignored so a bundle can be mounted without making the loader fail on
// its suite definition.
func (r *Registry) LoadFromDirectory(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read grader candidate %s: %w", path, readErr)
		}
		var genericDoc map[string]any
		if parseErr := yaml.Unmarshal(data, &genericDoc); parseErr != nil {
			return fmt.Errorf("parse grader candidate %s: %w", path, parseErr)
		}
		kind, _ := genericDoc["kind"].(string)
		if kind != "Grader" && kind != "DeterministicGraderSet" {
			return nil
		}
		if _, registerErr := r.Register(path); registerErr != nil {
			return fmt.Errorf("failed to register grader %s: %w", path, registerErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// calculateHash calculates canonical JSON hash (same as manifest compiler)
func (r *Registry) calculateHash(value any) (string, error) {
	return identity.HashCanonical(value)
}

// validateGrader validates grader configuration
func (r *Registry) validateGrader(grader *Grader) error {
	if grader.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}

	if grader.Kind != "Grader" {
		return fmt.Errorf("kind must be 'Grader', got '%s'", grader.Kind)
	}

	if grader.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	if grader.Spec.Type == "" {
		return fmt.Errorf("spec.type is required")
	}

	if grader.Spec.Method == "" {
		return fmt.Errorf("spec.method is required")
	}

	// Validate type and method combinations
	switch grader.Spec.Type {
	case GraderTypeDeterministic:
		if grader.Spec.Method != MethodFileExists &&
			grader.Spec.Method != MethodFileContent &&
			grader.Spec.Method != MethodFileHash &&
			grader.Spec.Method != MethodJSONSchema &&
			grader.Spec.Method != MethodJSONField &&
			grader.Spec.Method != MethodRegex &&
			grader.Spec.Method != MethodCommandExitCode &&
			grader.Spec.Method != MethodTraceAssertion &&
			grader.Spec.Method != MethodSuiteAssertions {
			return fmt.Errorf("invalid method '%s' for deterministic grader", grader.Spec.Method)
		}
	case GraderTypeLLM:
		if grader.Spec.Method != MethodLLMRubric {
			return fmt.Errorf("invalid method '%s' for llm grader", grader.Spec.Method)
		}
	default:
		return fmt.Errorf("unknown grader type: %s", grader.Spec.Type)
	}

	return nil
}
