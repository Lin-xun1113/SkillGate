package grader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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

	// Parse grader
	var grader Grader
	if err := yaml.Unmarshal(data, &grader); err != nil {
		return "", fmt.Errorf("failed to parse grader YAML: %w", err)
	}

	// Validate grader
	if err := r.validateGrader(&grader); err != nil {
		return "", fmt.Errorf("invalid grader: %w", err)
	}

	// Calculate hash
	hash := r.calculateHash(data)

	// Store in registry
	r.mu.Lock()
	r.graders[hash] = &grader
	r.mu.Unlock()

	return hash, nil
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

// calculateHash calculates SHA-256 hash of grader content
func (r *Registry) calculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
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
