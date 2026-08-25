package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
)

type SkillVersion struct {
	Name       string `json:"name"`
	Hash       string `json:"hash"`
	Canonical  []byte `json:"-"`
	Idempotent bool   `json:"idempotent"`
}

type SuiteVersion struct {
	Name       string `json:"name"`
	Hash       string `json:"hash"`
	Canonical  []byte `json:"-"`
	Idempotent bool   `json:"idempotent"`
}

type Version struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type Store interface {
	RegisterSkill(name, packageRoot string) (SkillVersion, error)
	GetSkill(name, hash string) (SkillVersion, error)
	RegisterSuite(name, suitePath string) (SuiteVersion, error)
	GetSuite(name, hash string) (SuiteVersion, error)
	ListVersions(kind, name string) ([]Version, error)
}

type FileStore struct{ root string }

func NewFileStore(root string) (*FileStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileStore{root: root}, nil
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func atomicCreate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".create-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Link(tmpName, path)
}

func rebuildIndex(root string) map[string][]Version {
	index := map[string][]Version{}
	for _, kind := range []string{"skill", "suite"} {
		base := filepath.Join(root, kind+"s")
		names, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, name := range names {
			if !name.IsDir() {
				continue
			}
			versions, err := os.ReadDir(filepath.Join(base, name.Name()))
			if err != nil {
				continue
			}
			key := kind + ":" + name.Name()
			for _, version := range versions {
				if version.IsDir() {
					index[key] = append(index[key], Version{Kind: kind, Name: name.Name(), Hash: version.Name()})
				}
			}
			sort.Slice(index[key], func(i, j int) bool { return index[key][i].Hash < index[key][j].Hash })
		}
	}
	return index
}

func (s *FileStore) updateIndex(kind, name, hash string) error {
	path := filepath.Join(s.root, "index.json")
	index := rebuildIndex(s.root)
	key := kind + ":" + name
	found := false
	for _, item := range index[key] {
		if item.Hash == hash {
			found = true
			break
		}
	}
	if !found {
		index[key] = append(index[key], Version{Kind: kind, Name: name, Hash: hash})
	}
	sort.Slice(index[key], func(i, j int) bool { return index[key][i].Hash < index[key][j].Hash })
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func (s *FileStore) RegisterSkill(name, packageRoot string) (SkillVersion, error) {
	if !validLogicalName(name) {
		return SkillVersion{}, WithCode("INVALID_SKILL_MANIFEST", fmt.Errorf("invalid logical name"))
	}
	if packageName, err := identity.SkillPackageName(packageRoot); err != nil {
		return SkillVersion{}, WithCode("INVALID_SKILL_MANIFEST", err)
	} else if packageName != name {
		return SkillVersion{}, WithCode("INVALID_SKILL_MANIFEST", fmt.Errorf("logical name does not match SKILL.md"))
	}
	canonical, err := identity.SkillPackageCanonical(packageRoot)
	if err != nil {
		return SkillVersion{}, WithCode("INVALID_SKILL_MANIFEST", err)
	}
	hash := identity.HashBytes(canonical)
	dir := filepath.Join(s.root, "skills", name, hash)
	file := filepath.Join(dir, "canonical.json")
	if old, err := os.ReadFile(file); err == nil {
		if string(old) != string(canonical) {
			return SkillVersion{}, fmt.Errorf("registry conflict")
		}
		if err := s.updateIndex("skill", name, hash); err != nil {
			return SkillVersion{}, err
		}
		return SkillVersion{Name: name, Hash: hash, Canonical: old, Idempotent: true}, nil
	}
	if err := atomicCreate(file, canonical); err != nil {
		if existing, readErr := os.ReadFile(file); readErr == nil {
			if string(existing) == string(canonical) {
				if indexErr := s.updateIndex("skill", name, hash); indexErr != nil {
					return SkillVersion{}, indexErr
				}
				return SkillVersion{Name: name, Hash: hash, Canonical: existing, Idempotent: true}, nil
			}

			return SkillVersion{}, fmt.Errorf("registry conflict: %w", err)
		}
		return SkillVersion{}, err
	}
	meta, _ := json.Marshal(map[string]any{"name": name, "hash": hash})
	if err := atomicWrite(filepath.Join(dir, "metadata.json"), meta); err != nil {
		return SkillVersion{}, err
	}
	if err := s.updateIndex("skill", name, hash); err != nil {
		return SkillVersion{}, err
	}
	return SkillVersion{Name: name, Hash: hash, Canonical: canonical}, nil
}

func validLogicalName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (i > 0 && r == '-') {
			continue
		}
		return false
	}
	return name[0] >= 'a' && name[0] <= 'z'
}

func (s *FileStore) GetSkill(name, hash string) (SkillVersion, error) {
	b, err := os.ReadFile(filepath.Join(s.root, "skills", name, hash, "canonical.json"))
	if err != nil {
		return SkillVersion{}, err
	}
	return SkillVersion{Name: name, Hash: hash, Canonical: b}, nil
}

func (s *FileStore) ListSkills(name string) ([]SkillVersion, error) {
	versions, err := s.ListVersions("skill", name)
	if err != nil {
		return nil, err
	}
	out := make([]SkillVersion, 0, len(versions))
	for _, version := range versions {
		item, err := s.GetSkill(name, version.Hash)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *FileStore) ListVersions(kind, name string) ([]Version, error) {
	base := filepath.Join(s.root, kind+"s", name)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, Version{Kind: kind, Name: name, Hash: entry.Name()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out, nil
}
