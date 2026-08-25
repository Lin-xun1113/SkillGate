package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/validation"
	"gopkg.in/yaml.v3"
)

func decodeYAML(data []byte, target *any) error { return yaml.Unmarshal(data, target) }

func (s *FileStore) RegisterSuite(name, suitePath string) (SuiteVersion, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`).MatchString(name) {
		return SuiteVersion{}, fmt.Errorf("invalid suite name")
	}
	data, err := os.ReadFile(suitePath)
	if err != nil {
		if os.IsNotExist(err) {
			return SuiteVersion{}, WithCode("FILE_NOT_FOUND", err)
		}
		return SuiteVersion{}, WithCode("IO_ERROR", err)
	}
	result, diagnostics := validation.ValidateSuite(suitePath, filepath.Dir(suitePath))
	if len(diagnostics) > 0 {
		return SuiteVersion{}, WithCode(diagnostics[0].Code, fmt.Errorf("suite validation failed: %s", diagnostics[0].Code))
	}
	if result.Name != name {
		return SuiteVersion{}, WithCode("INVALID_ARGUMENT", fmt.Errorf("suite logical name mismatch"))
	}
	var parsed any
	if err := decodeYAML(data, &parsed); err != nil {
		return SuiteVersion{}, fmt.Errorf("parse suite: %w", err)
	}
	canonical, err := identity.CanonicalJSON(parsed)
	if err != nil {
		return SuiteVersion{}, err
	}
	hash := identity.HashBytes(canonical)
	dir := filepath.Join(s.root, "suites", name, hash)
	file := filepath.Join(dir, "canonical.json")
	if old, err := os.ReadFile(file); err == nil {
		if string(old) != string(canonical) {
			return SuiteVersion{}, fmt.Errorf("registry conflict")
		}
		if err := s.updateIndex("suite", name, hash); err != nil {
			return SuiteVersion{}, err
		}
		return SuiteVersion{Name: name, Hash: hash, Canonical: old, Idempotent: true}, nil
	}
	if err := atomicCreate(file, canonical); err != nil {
		if existing, readErr := os.ReadFile(file); readErr == nil && string(existing) == string(canonical) {
			return SuiteVersion{Name: name, Hash: hash, Canonical: existing, Idempotent: true}, nil
		}
		return SuiteVersion{}, err
	}
	meta, _ := json.Marshal(map[string]any{"name": name, "hash": hash})
	if err := atomicWrite(filepath.Join(dir, "metadata.json"), meta); err != nil {
		return SuiteVersion{}, err
	}
	if err := s.updateIndex("suite", name, hash); err != nil {
		return SuiteVersion{}, err
	}
	return SuiteVersion{Name: name, Hash: hash, Canonical: canonical}, nil
}

func (s *FileStore) GetSuite(name, hash string) (SuiteVersion, error) {
	b, err := os.ReadFile(filepath.Join(s.root, "suites", name, hash, "canonical.json"))
	if err != nil {
		return SuiteVersion{}, err
	}
	return SuiteVersion{Name: name, Hash: hash, Canonical: b}, nil
}

func (s *FileStore) ListSuites(name string) ([]SuiteVersion, error) {
	versions, err := s.ListVersions("suite", name)
	if err != nil {
		return nil, err
	}
	out := make([]SuiteVersion, 0, len(versions))
	for _, version := range versions {
		item, err := s.GetSuite(name, version.Hash)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
