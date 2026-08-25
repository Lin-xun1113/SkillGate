package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type PairInput struct {
	ExperimentHash  string `json:"experiment_hash"`
	SuiteHash       string `json:"suite_hash"`
	CaseID          string `json:"case_id"`
	EvaluationMode  string `json:"evaluation_mode"`
	Repetition      int    `json:"repetition"`
	Treatment       string `json:"treatment"`
	BaselineArm     string `json:"baseline_arm"`
	CandidateArm    string `json:"candidate_arm"`
	ModelHash       string `json:"model_hash"`
	HarnessHash     string `json:"harness_hash"`
	EnvironmentHash string `json:"environment_hash"`
	FixtureHash     string `json:"fixture_hash"`
	GraderHash      string `json:"grader_hash"`
	ToolPolicyHash  string `json:"tool_policy_hash"`
}

func CanonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "")
	if err := encoder.Encode(normalize(value)); err != nil {
		return nil, err
	}
	encoded := buffer.Bytes()
	if len(encoded) > 0 && encoded[len(encoded)-1] == '\n' {
		encoded = encoded[:len(encoded)-1]
	}
	return encoded, nil
}

func normalize(value any) any {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(strings.ReplaceAll(v, "\r\n", "\n"), "\r", "\n")
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalize(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalize(item)
		}
		return out
	default:
		return value
	}
}

func HashCanonical(value any) (string, error) {
	bytes, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return HashBytes(bytes), nil
}

func HashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type canonicalFile struct {
	Path     string `json:"path"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func SkillPackageCanonical(root string) ([]byte, error) {
	if err := ValidateSkillPackage(root); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve package root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat package root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("package root is not a directory")
	}
	files := make([]canonicalFile, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return fmt.Errorf("path outside package root: %s", rel)
		}
		rel = filepath.ToSlash(rel)
		if !utf8.ValidString(rel) {
			return fmt.Errorf("invalid UTF-8 relative path: %q", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if utf8.Valid(data) && !bytes.Contains(data, []byte{0}) {
			content := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
			files = append(files, canonicalFile{Path: rel, Encoding: "utf8", Content: content})
		} else {
			files = append(files, canonicalFile{Path: rel, Encoding: "base64", Content: base64.StdEncoding.EncodeToString(data)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return CanonicalJSON(map[string]any{"format": "skillgate.skill-package.v1", "files": files})
}

func SkillPackageHash(root string) (string, error) {
	canonical, err := SkillPackageCanonical(root)
	if err != nil {
		return "", err
	}
	return HashBytes(canonical), nil
}

// SkillPackageLegacyHash preserves the M0 package identity while M1 accepts
// already-frozen M0 manifests. New registrations always use SkillPackageHash.
// SKILL.md remains opaque for package identity. Frontmatter is validated by
// ValidateSkillPackage, while the package hash only normalizes file bytes.

func SkillPackageLegacyHash(root string) (string, error) {
	if err := ValidateSkillPackage(root); err != nil {
		return "", err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	files := make([]map[string]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !utf8.ValidString(rel) || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid package path: %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("legacy M0 package requires UTF-8 files: %s", rel)
		}
		files = append(files, map[string]string{"path": filepath.ToSlash(rel), "content": strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i]["path"] < files[j]["path"] })
	canonical, err := CanonicalJSON(map[string]any{"files": files})
	if err != nil {
		return "", err
	}
	return HashBytes(canonical), nil
}

func PairID(input PairInput) (string, error) { return HashCanonical(input) }

func TrialID(pairID, arm string, attempt int) (string, error) {
	return HashCanonical(map[string]any{"pair_id": pairID, "arm": arm, "attempt": attempt})
}
