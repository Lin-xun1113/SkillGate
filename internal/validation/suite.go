package validation

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"gopkg.in/yaml.v3"
)

type SuiteCase struct {
	ID             string
	EvaluationMode string
	Population     string
	Fixtures       []map[string]any
}

type SuiteResult struct {
	Name      string
	Hash      string
	Canonical []byte
	Document  map[string]any
	Cases     []SuiteCase
}

var (
	logicalNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	secretKeyPattern    = regexp.MustCompile(`(?i)^(api[_-]?key|token|password|passwd|client[_-]?secret|secret[_-]?access[_-]?key|private[_-]?key|credential|credentials)$`)
	secretValuePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^sk-[A-Za-z0-9]{10,}$`),
		regexp.MustCompile(`^AKIA[0-9A-Z]{16}$`),
		regexp.MustCompile(`-----BEGIN [^-]+ PRIVATE KEY-----`),
	}
)

func ValidateSuite(path, projectRoot string) (SuiteResult, []Diagnostic) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SuiteResult{}, []Diagnostic{{Code: "FILE_NOT_FOUND", Severity: "error", Path: path, Message: "Suite 文件不存在。"}}
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return SuiteResult{}, []Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: path, Message: "Suite YAML 无法解析。"}}
	}
	var diagnostics []Diagnostic
	if document["apiVersion"] != "skillgate.dev/v1alpha1" || document["kind"] != "EvalSuite" {
		diagnostics = append(diagnostics, Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "Suite apiVersion 或 kind 不正确。"})
	}
	if secretPath := FindSecretField(document, ""); secretPath != "" {
		diagnostics = append(diagnostics, Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: secretPath, Message: "Suite 包含 Secret 字段。"})
	}
	metadata, _ := document["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	if !logicalNamePattern.MatchString(name) {
		diagnostics = append(diagnostics, Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "metadata.name", Message: "Suite name 必须为 kebab-case。"})
	}
	spec, _ := document["spec"].(map[string]any)
	rawCases, _ := spec["cases"].([]any)
	seen := map[string]bool{}
	cases := make([]SuiteCase, 0, len(rawCases))
	root, _ := filepath.Abs(projectRoot)
	suiteDir, _ := filepath.Abs(filepath.Dir(path))

	for index, raw := range rawCases {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["id"].(string)
		casePath := "spec.cases[" + itoa(index) + "]"
		if id == "" || seen[id] {
			diagnostics = append(diagnostics, Diagnostic{Code: "DUPLICATE_CASE_ID", Severity: "error", Path: casePath + ".id", Message: "Case ID 为空或重复。"})
		}
		seen[id] = true
		mode, _ := item["evaluationMode"].(string)
		population, _ := item["population"].(string)
		if !validModePopulation(mode, population) {
			diagnostics = append(diagnostics, Diagnostic{Code: "INVALID_EVALUATION_MODE", Severity: "error", Path: casePath, Message: "evaluationMode 与 population 组合非法。"})
		}

		if isAnswerKeyReference(stringValue(item["prompt"])) || isAnswerKeyReference(stringValue(item["context"])) || isAnswerKeyReference(stringValue(item["agentContext"])) {
			diagnostics = append(diagnostics, Diagnostic{Code: "LEAKAGE_DETECTED", Severity: "error", Path: casePath, Message: "Prompt/Context 不能引用 Expected/Answer Key。"})
		}
		for _, visible := range stringSlice(item["agentVisible"]) {
			if isAnswerKeyReference(visible) {
				diagnostics = append(diagnostics, Diagnostic{Code: "LEAKAGE_DETECTED", Severity: "error", Path: casePath + ".agentVisible", Message: "Expected/Answer Key 不能进入 Agent-visible Context。"})
				continue
			}
			if looksLikeReference(visible) {
				diagnostics = append(diagnostics, validateReference(suiteDir, root, visible, "", casePath+".agentVisible", false)...)
			}
		}

		fixtures := mapSlice(item["fixtures"])
		for fixtureIndex, fixture := range fixtures {
			ref := stringValue(fixture["path"])
			declared := stringValue(fixture["sha256"])
			if declared == "" {
				diagnostics = append(diagnostics, Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: casePath + ".fixtures[" + itoa(fixtureIndex) + "].sha256", Message: "Fixture 必须声明内容 Hash。"})
			}
			diagnostics = append(diagnostics, validateReference(suiteDir, root, ref, declared, casePath+".fixtures["+itoa(fixtureIndex)+"]", false)...)
		}

		for outputIndex, output := range mapSlice(item["outputs"]) {
			ref := stringValue(output["path"])
			if code := ValidateWorkspacePath(ref); code != "" {
				diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: casePath + ".outputs[" + itoa(outputIndex) + "].path", Message: "Output 路径必须是 Workspace-relative 且不能指向 Answer Key。"})
			}
		}

		for assertionIndex, assertion := range mapSlice(item["assertions"]) {
			for _, field := range []string{"schemaRef", "expectedRef"} {
				ref := stringValue(assertion[field])
				if ref == "" {
					continue
				}
				declared := stringValue(assertion[field+"Hash"])
				if declared == "" && field == "schemaRef" {
					declared = stringValue(assertion["sha256"])
				}
				diagnostics = append(diagnostics, validateReference(suiteDir, root, ref, declared, casePath+".assertions["+itoa(assertionIndex)+"]."+field, true)...)
			}
		}

		if security, ok := item["security"].(map[string]any); ok {
			if evidence, ok := security["evidence"].(map[string]any); ok {
				for _, field := range []string{"findingRef", "traceRef", "evidenceRef"} {
					ref := stringValue(evidence[field])
					if ref == "" {
						continue
					}
					declared := stringValue(evidence[field+"Hash"])
					diagnostics = append(diagnostics, validateReference(suiteDir, root, ref, declared, casePath+".security.evidence."+field, true)...)
				}
			}
		}
		cases = append(cases, SuiteCase{ID: id, EvaluationMode: mode, Population: population, Fixtures: fixtures})
	}

	canonical, err := identity.CanonicalJSON(document)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path, Message: "Suite 无法 Canonicalize。"})
	}
	return SuiteResult{Name: name, Hash: identity.HashBytes(canonical), Canonical: canonical, Document: document, Cases: cases}, diagnostics
}

func validateReference(base, root, ref, declaredHash, path string, structured bool) []Diagnostic {
	if ref == "" {
		return []Diagnostic{{Code: "FILE_NOT_FOUND", Severity: "error", Path: path, Message: "引用路径不能为空。"}}
	}
	resolved, code := ResolveReference(base, root, ref)
	if code != "" {
		return []Diagnostic{{Code: code, Severity: "error", Path: path, Message: "引用路径非法。"}}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return []Diagnostic{{Code: "FILE_NOT_FOUND", Severity: "error", Path: path, Message: "引用文件不存在。"}}
	}
	var diagnostics []Diagnostic
	if declaredHash != "" && declaredHash != identity.HashBytes(data) {
		diagnostics = append(diagnostics, Diagnostic{Code: "CONTENT_HASH_MISMATCH", Severity: "error", Path: path + ".hash", Message: "引用 Hash 与内容不一致。"})
	}
	if structured || isStructuredPath(ref) {
		var parsed any
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path, Message: "结构化引用无法解析。"})
		} else if secret := FindSecretField(parsed, ""); secret != "" {
			diagnostics = append(diagnostics, Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: path + "." + secret, Message: "引用内容包含 Secret 字段或明显凭据值。"})
		}
	}
	return diagnostics
}

func ResolveReference(base, projectRoot, ref string) (string, string) {
	if ref == "" {
		return "", "FILE_NOT_FOUND"
	}
	if filepath.IsAbs(ref) || strings.HasPrefix(ref, "~") {
		return "", "PATH_OUTSIDE_ROOT"
	}
	clean := filepath.Clean(filepath.FromSlash(ref))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "PATH_OUTSIDE_ROOT"
	}
	resolved, _ := filepath.Abs(filepath.Join(base, clean))
	root, _ := filepath.Abs(projectRoot)
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "PATH_OUTSIDE_ROOT"
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			break
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", "PATH_OUTSIDE_ROOT"
		}
	}
	return resolved, ""
}

func FindSecretField(value any, prefix string) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			item := typed[key]
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if secretKeyPattern.MatchString(key) {
				if text, ok := item.(string); ok && strings.Contains(strings.ToLower(text), "none") {
					// Explicit no-secret sentinels are allowed in offline fixtures.
				} else {
					return path
				}
			}
			if text, ok := item.(string); ok {
				for _, pattern := range secretValuePatterns {
					if pattern.MatchString(text) {
						return path
					}
				}
			}
			if found := FindSecretField(item, path); found != "" {
				return found
			}
		}
	case []any:
		for index, item := range typed {
			if found := FindSecretField(item, prefix+"["+itoa(index)+"]"); found != "" {
				return found
			}
		}
	}
	return ""
}

func ValidateWorkspacePath(path string) string {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return "PATH_OUTSIDE_ROOT"
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "PATH_OUTSIDE_ROOT"
	}
	if isAnswerKeyReference(path) {
		return "LEAKAGE_DETECTED"
	}
	return ""
}

func isAnswerKeyReference(value string) bool {
	lower := strings.ToLower(filepath.ToSlash(value))
	for _, token := range []string{"expected/", "answer-key", "answer_key", "grader-only", "grader_only"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return strings.Contains(value, "..")
}

func looksLikeReference(value string) bool {
	return strings.Contains(value, "/") || strings.Contains(value, "\\") || filepath.Ext(value) != ""
}

func isStructuredPath(ref string) bool {
	ext := strings.ToLower(filepath.Ext(ref))
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func stringSlice(value any) []string {
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, raw := range list {
		if item, ok := raw.(string); ok {
			out = append(out, item)
		}
	}
	return out
}

func mapSlice(value any) []map[string]any {
	list, _ := value.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		if item, ok := raw.(map[string]any); ok {
			out = append(out, item)
		}
	}
	return out
}

func validModePopulation(mode, population string) bool {
	return (mode == "forced_injection" && population == "answer") ||
		(mode == "autonomous_trigger" && population == "trigger") ||
		(mode == "security_probe" && population == "answer")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var out [20]byte
	index := len(out)
	for value > 0 {
		index--
		out[index] = byte('0' + value%10)
		value /= 10
	}
	return string(out[index:])
}
