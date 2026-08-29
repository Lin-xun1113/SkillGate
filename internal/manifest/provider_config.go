package manifest

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Lin-xun1113/SkillGate/internal/validation"
)

// Provider model configuration is deliberately small.  These limits are part
// of the execution identity and also keep an untrusted manifest from asking a
// provider SDK for an unbounded amount of work.
const (
	providerMaxNameLength     = 256
	providerMaxTemperature    = 2.0
	providerMaxTokens         = 1_000_000
	providerMaxTimeoutSeconds = 600.0
	providerMaxRetries        = 30
)

var providerTypes = map[string]struct{}{
	"fixture":   {},
	"mock":      {},
	"recorded":  {},
	"openai":    {},
	"anthropic": {},
}

// Provider model fields accepted at the model level.  type/model/model_id
// and params are retained only for compatibility with older projections.
var providerModelFields = map[string]struct{}{
	"provider": {}, "type": {}, "name": {}, "model": {}, "model_id": {},
	"config": {}, "params": {}, "recordings_path": {},
}

// Canonical names accepted in config and legacy params.  The camelCase forms
// are read-only compatibility aliases; they are normalized by the Worker.
var providerConfigFields = map[string]string{
	"temperature":     "temperature",
	"max_tokens":      "max_tokens",
	"maxTokens":       "max_tokens",
	"timeout_seconds": "timeout_seconds",
	"timeoutSeconds":  "timeout_seconds",
	"timeout":         "timeout_seconds",
	"max_retries":     "max_retries",
	"maxRetries":      "max_retries",
}

// validateProviderModel validates one strategy's model object.  It returns
// diagnostics instead of an error so Compiler callers retain the existing
// diagnostic contract.
func validateProviderModel(value any, path string) []validation.Diagnostic {
	diagnostics := make([]validation.Diagnostic, 0)
	model, ok := providerStringMap(value)
	if !ok {
		return append(diagnostics, providerDiagnostic(path, "model 必须是对象。"))
	}

	keys := make([]string, 0, len(model))
	for key := range model {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, allowed := providerModelFields[key]; !allowed {
			diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, key), "model 包含未允许字段。"))
		}
	}

	provider, providerPresent := providerStringField(model, "provider")
	typeValue, typePresent := providerStringField(model, "type")
	if providerPresent && typePresent && !strings.EqualFold(provider, typeValue) {
		diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, "provider"), "provider 与兼容字段 type 不一致。"))
	}
	if !providerPresent {
		provider = typeValue
		providerPresent = typePresent
	}
	if !providerPresent {
		diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, "provider"), "provider 必须是字符串。"))
	} else if _, allowed := providerTypes[strings.ToLower(strings.TrimSpace(provider))]; !allowed {
		diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, "provider"), "provider 类型不受支持。"))
	}

	nameValues := make([]struct {
		key   string
		value string
	}, 0, 3)
	for _, key := range []string{"name", "model_id", "model"} {
		if raw, exists := model[key]; exists {
			name, ok := raw.(string)
			if !ok || strings.TrimSpace(name) == "" {
				diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, key), "model name 必须是非空字符串。"))
				continue
			}
			name = strings.TrimSpace(name)
			if len(name) > providerMaxNameLength {
				diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, key), "model name 超出长度限制。"))
				continue
			}
			nameValues = append(nameValues, struct {
				key   string
				value string
			}{key, name})
		}
	}
	if len(nameValues) == 0 {
		diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, "name"), "model name 必须是非空字符串。"))
	} else {
		for _, item := range nameValues[1:] {
			if item.value != nameValues[0].value {
				diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, item.key), "兼容 model name 字段不一致。"))
			}
		}
	}

	sections := make([]struct {
		name  string
		value any
	}, 0, 2)
	for _, sectionName := range []string{"config", "params"} {
		if raw, exists := model[sectionName]; exists {
			if _, ok := providerStringMap(raw); !ok {
				diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, sectionName), "model config 必须是对象。"))
				continue
			}
			sections = append(sections, struct {
				name  string
				value any
			}{sectionName, raw})
		}
	}
	seen := map[string]string{}
	for _, section := range sections {
		sectionMap, _ := providerStringMap(section.value)
		sectionKeys := make([]string, 0, len(sectionMap))
		for key := range sectionMap {
			sectionKeys = append(sectionKeys, key)
		}
		sort.Strings(sectionKeys)
		for _, key := range sectionKeys {
			canonical, allowed := providerConfigFields[key]
			fieldPath := joinProviderPath(joinProviderPath(path, section.name), key)
			if !allowed {
				diagnostics = append(diagnostics, providerDiagnostic(fieldPath, "model.config 包含未允许字段。"))
				continue
			}
			if previous, exists := seen[canonical]; exists {
				diagnostics = append(diagnostics, providerDiagnostic(fieldPath, fmt.Sprintf("model config 字段与 %s 重复。", previous)))
				continue
			}
			seen[canonical] = fieldPath
			diagnostics = append(diagnostics, validateProviderConfigValue(canonical, sectionMap[key], fieldPath)...)
		}
	}

	// Flat provider parameter fields are not part of the model contract.  Keep
	// the rejection explicit so callers do not accidentally bypass config.
	for _, key := range []string{"temperature", "max_tokens", "maxTokens", "timeout_seconds", "timeoutSeconds", "max_retries", "maxRetries"} {
		if _, exists := model[key]; exists {
			diagnostics = append(diagnostics, providerDiagnostic(joinProviderPath(path, key), "Provider 参数必须放在 model.config 中。"))
		}
	}
	return diagnostics
}

func validateProviderConfigValue(canonical string, value any, path string) []validation.Diagnostic {
	bad := func(message string) []validation.Diagnostic {
		return []validation.Diagnostic{providerDiagnostic(path, message)}
	}
	switch canonical {
	case "temperature":
		number, ok := providerNumber(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > providerMaxTemperature {
			return bad("temperature 必须是 0..2 的有限数值。")
		}
	case "max_tokens":
		number, ok := providerInteger(value)
		if !ok || number < 1 || number > providerMaxTokens {
			return bad("max_tokens 必须是 1..1000000 的整数。")
		}
	case "timeout_seconds":
		number, ok := providerNumber(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 || number > providerMaxTimeoutSeconds {
			return bad("timeout_seconds 必须是大于 0 且不超过 600 的有限数值。")
		}
	case "max_retries":
		number, ok := providerInteger(value)
		if !ok || number < 0 || number > providerMaxRetries {
			return bad("max_retries 必须是 0..30 的整数。")
		}
	}
	return nil
}

func providerStringMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if typed, ok := value.(map[string]any); ok {
		return typed, true
	}
	return nil, false
}

func providerStringField(model map[string]any, key string) (string, bool) {
	value, exists := model[key]
	if !exists {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func providerNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func providerInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}

func providerDiagnostic(path, message string) validation.Diagnostic {
	return validation.Diagnostic{Code: "INVALID_PROVIDER_CONFIG", Severity: "error", Path: path, Message: message}
}

func joinProviderPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
}
