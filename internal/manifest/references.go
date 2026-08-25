package manifest

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/Lin-xun1113/SkillGate/internal/validation"
	"gopkg.in/yaml.v3"
)

func validateGraderReferences(graderPath, projectRoot string) []validation.Diagnostic {
	data, err := os.ReadFile(graderPath)
	if err != nil {
		return []validation.Diagnostic{{Code: "FILE_NOT_FOUND", Severity: "error", Path: graderPath, Message: "Grader 文件不存在。"}}
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: graderPath, Message: "Grader 无法解析。"}}
	}
	spec, _ := document["spec"].(map[string]any)
	base := filepath.Dir(graderPath)
	var diagnostics []validation.Diagnostic
	for _, field := range []string{"suiteRef"} {
		if ref, ok := spec[field].(string); ok && ref != "" {
			diagnostics = append(diagnostics, validateManifestReference(base, projectRoot, ref, graderPath+"."+field)...)
		}
	}
	for _, field := range []string{"schemaRefs", "expectedRefs"} {
		list, _ := spec[field].([]any)
		for index, raw := range list {
			if ref, ok := raw.(string); ok {
				diagnostics = append(diagnostics, validateManifestReference(base, projectRoot, ref, graderPath+"."+field+"["+strconv.Itoa(index)+"]")...)
			}
		}
	}
	return diagnostics
}

func resolveManifestReference(base, projectRoot, ref string) (string, string) {
	resolved, code := validation.ResolveReference(base, projectRoot, ref)
	if code != "" {
		return "", code
	}
	if _, err := os.Stat(resolved); err == nil {
		return resolved, ""
	}
	fallback, fallbackCode := validation.ResolveReference(projectRoot, projectRoot, ref)
	if fallbackCode != "" {
		return "", fallbackCode
	}
	if _, err := os.Stat(fallback); err == nil {
		return fallback, ""
	}
	return resolved, ""
}

func validateManifestReference(base, projectRoot, ref, path string) []validation.Diagnostic {
	resolved, code := resolveManifestReference(base, projectRoot, ref)
	if code != "" {
		return []validation.Diagnostic{{Code: code, Severity: "error", Path: path, Message: "引用路径非法。"}}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return []validation.Diagnostic{{Code: "FILE_NOT_FOUND", Severity: "error", Path: path, Message: "引用文件不存在。"}}
	}
	var diagnostics []validation.Diagnostic
	if ext := filepath.Ext(ref); ext == ".yaml" || ext == ".yml" || ext == ".json" {
		var parsed any
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path, Message: "结构化引用无法解析。"})
		} else if secret := validation.FindSecretField(parsed, ""); secret != "" {
			diagnostics = append(diagnostics, validation.Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: path + "." + secret, Message: "引用内容包含 Secret 字段或明显凭据值。"})
		}
	}
	return diagnostics
}

func validateManifestReferences(spec map[string]any, manifestPath, projectRoot string) []validation.Diagnostic {
	base := filepath.Dir(manifestPath)
	var diagnostics []validation.Diagnostic
	if pairing, ok := spec["pairing"].(map[string]any); ok {
		if ref, ok := pairing["identityExamples"].(string); ok && ref != "" {
			diagnostics = append(diagnostics, validateManifestReference(base, projectRoot, ref, "spec.pairing.identityExamples")...)
		}
	}
	if grading, ok := spec["grading"].(map[string]any); ok {
		if list, ok := grading["deterministic"].([]any); ok {
			for i, raw := range list {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if ref, ok := item["suiteRef"].(string); ok && ref != "" {
					diagnostics = append(diagnostics, validateManifestReference(base, projectRoot, ref, "spec.grading.deterministic["+strconv.Itoa(i)+"].suiteRef")...)
				}
				if refs, ok := item["schemaRefs"].([]any); ok {
					for j, rawRef := range refs {
						if ref, ok := rawRef.(string); ok {
							diagnostics = append(diagnostics, validateManifestReference(base, projectRoot, ref, "spec.grading.deterministic["+strconv.Itoa(i)+"].schemaRefs["+strconv.Itoa(j)+"]")...)
						}
					}
				}
			}
		}
	}
	return diagnostics
}
