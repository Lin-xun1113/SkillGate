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
			Type:   GraderTypeDeterministic,
			Method: MethodJSONSchema, // DeterministicGraderSet uses JSON schema validation
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

	return grader, nil
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

// LoadFromDirectory loads all grader files from a directory
func (r *Registry) LoadFromDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		if _, err := r.Register(path); err != nil {
			return fmt.Errorf("failed to register grader %s: %w", entry.Name(), err)
		}
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
		if grader.Spec.Method != MethodFileContent &&
			grader.Spec.Method != MethodJSONSchema &&
			grader.Spec.Method != MethodCommandExitCode &&
			grader.Spec.Method != MethodTraceAssertion {
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
