package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"gopkg.in/yaml.v3"
)

// casCommand materializes the immutable inputs needed by a worker. The
// scheduler stores only hashes; this command is the explicit boundary that
// turns a source manifest/package into a shared, read-only CAS volume.
func casCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	if len(args) == 0 || args[0] != "prepare" {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "cas", "仅支持 cas prepare")
	}
	manifestPath := positional(args[1:])
	if manifestPath == "" {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "cas.prepare", "Manifest 路径不能为空")
	}
	projectRoot := flagValue(args, "--project-root", "")
	if projectRoot == "" {
		var err error
		projectRoot, err = os.Getwd()
		if err != nil {
			return returnWithError(jsonOutput, "IO_ERROR", "project-root", err.Error())
		}
	}
	casRoot := flagValue(args, "--cas-dir", filepath.Join(projectRoot, ".skillgate", "cas"))
	compiled, diagnostics := manifest.Compile(manifestPath, projectRoot)
	if len(diagnostics) > 0 || compiled == nil {
		return returnDiagnostics(jsonOutput, diagnostics)
	}
	if err := materializeCompiledCAS(projectRoot, manifestPath, casRoot, compiled); err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", "cas.prepare", err.Error())
	}
	return printOperationSuccess(jsonOutput, "cas.prepare", map[string]any{
		"manifest_hash": compiled.ManifestHash,
		"cas_dir":       casRoot,
		"skills":        1,
	})
}

func materializeCompiledCAS(projectRoot, manifestPath, casRoot string, compiled *manifest.CompiledExperiment) error {
	if compiled == nil || compiled.ManifestHash == "" {
		return fmt.Errorf("compiled manifest identity is empty")
	}
	digest := strings.TrimPrefix(compiled.ManifestHash, "sha256:")
	if len(digest) < 2 {
		return fmt.Errorf("invalid manifest hash: %s", compiled.ManifestHash)
	}
	compiledBytes, err := json.MarshalIndent(compiled, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal compiled manifest: %w", err)
	}
	manifestDir := filepath.Join(casRoot, "manifests", digest[:2])
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("create manifest CAS directory: %w", err)
	}
	if err := atomicCASWrite(filepath.Join(manifestDir, compiled.ManifestHash+".json"), compiledBytes); err != nil {
		return fmt.Errorf("write compiled manifest: %w", err)
	}

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("parse source manifest: %w", err)
	}
	spec, _ := document["spec"].(map[string]any)
	skillList, _ := spec["skills"].([]any)
	for _, value := range skillList {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name, _ := item["name"].(string)
		declared, _ := item["version"].(string)
		ref, _ := item["path"].(string)
		if name == "" || declared == "" || ref == "" {
			return fmt.Errorf("skill reference must contain name, path and version")
		}
		skillPath := resolveCASReference(filepath.Dir(manifestPath), projectRoot, ref)
		if err := materializeSkillCAS(casRoot, name, declared, skillPath); err != nil {
			return err
		}
	}
	return nil
}

func resolveCASReference(base, projectRoot, ref string) string {
	if filepath.IsAbs(ref) {
		return filepath.Clean(ref)
	}
	candidate := filepath.Clean(filepath.Join(base, ref))
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return filepath.Clean(filepath.Join(projectRoot, ref))
}

func materializeSkillCAS(casRoot, name, declaredHash, sourceRoot string) error {
	canonical, err := identity.SkillPackageCanonical(sourceRoot)
	if err != nil {
		return fmt.Errorf("canonicalize skill %s: %w", name, err)
	}
	actualHash := identity.HashBytes(canonical)
	if declaredHash != actualHash {
		legacy, legacyErr := identity.SkillPackageLegacyHash(sourceRoot)
		if legacyErr != nil || declaredHash != legacy {
			return fmt.Errorf("skill %s hash mismatch: declared %s, actual %s", name, declaredHash, actualHash)
		}
		// M0 manifests use the legacy archive identity. Store the exact legacy
		// canonical bytes under that key so the worker can verify the declared
		// hash instead of accepting a differently encoded package.
		canonical, err = legacySkillCanonical(sourceRoot)
		if err != nil {
			return fmt.Errorf("canonicalize legacy skill %s: %w", name, err)
		}
	}
	digest := strings.TrimPrefix(declaredHash, "sha256:")
	if len(digest) < 2 {
		return fmt.Errorf("invalid skill hash: %s", declaredHash)
	}
	dir := filepath.Join(casRoot, "skills", digest[:2], declaredHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create skill CAS directory: %w", err)
	}
	if err := atomicCASWrite(filepath.Join(dir, "canonical.json"), canonical); err != nil {
		return fmt.Errorf("write skill canonical archive: %w", err)
	}
	metadata := map[string]any{"name": name, "version": declaredHash, "content_hash": actualHash}
	if err := atomicCASWriteJSON(filepath.Join(dir, "metadata.json"), metadata); err != nil {
		return fmt.Errorf("write skill metadata: %w", err)
	}
	// Keep the original package files available to Python tools. The generated
	// skill.yaml is a safe projection, not a replacement for SKILL.md.
	var skillText string
	if data, readErr := os.ReadFile(filepath.Join(sourceRoot, "SKILL.md")); readErr == nil {
		skillText = string(data)
	}
	projection := map[string]any{"metadata": metadata, "content": skillText}
	projectionBytes, err := yaml.Marshal(projection)
	if err != nil {
		return fmt.Errorf("marshal skill projection: %w", err)
	}
	if err := atomicCASWrite(filepath.Join(dir, "skill.yaml"), projectionBytes); err != nil {
		return fmt.Errorf("write skill projection: %w", err)
	}
	return copySkillFiles(sourceRoot, dir)
}

func legacySkillCanonical(sourceRoot string) ([]byte, error) {
	files := make([]map[string]string, 0)
	err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill package contains symbolic link: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("legacy skill package requires UTF-8 files: %s", path)
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		files = append(files, map[string]string{"path": filepath.ToSlash(rel), "content": strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i]["path"] < files[j]["path"] })
	return identity.CanonicalJSON(map[string]any{"files": files})
}

func copySkillFiles(sourceRoot, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill package contains symbolic link: %s", path)
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		return atomicCASCopy(filepath.Join(targetRoot, rel), path)
	})
}

func atomicCASWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".cas-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	// os.CreateTemp uses 0600 by default. CAS volumes are commonly produced by
	// the root bootstrap container and mounted read-only by an unprivileged
	// worker, so published blobs must be world-readable while remaining
	// immutable after the atomic rename.
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != string(data) {
			return fmt.Errorf("CAS path already contains different content: %s", path)
		}
		return nil
	}
	return os.Rename(tmp, path)
}

func atomicCASCopy(target, source string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return atomicCASWrite(target, data)
}

func atomicCASWriteJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicCASWrite(path, data)
}
