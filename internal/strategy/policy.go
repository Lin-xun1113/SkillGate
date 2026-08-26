package strategy

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/validation"
	"gopkg.in/yaml.v3"
)

const (
	DecisionPromote Decision = "PROMOTE"
	DecisionHold    Decision = "HOLD"
	DecisionReject  Decision = "REJECT"

	maxExpressionBytes = 2048
	policyAPIVersion   = "skillgate.dev/v1alpha1"
	policyKind         = "ReleasePolicy"
)

// Decision is a release outcome.
type Decision string

func (d Decision) Valid() bool {
	switch d {
	case DecisionPromote, DecisionHold, DecisionReject:
		return true
	default:
		return false
	}
}

// HashPolicy returns the canonical Policy Hash.
func HashPolicy(policy *Policy) (string, error) {
	if policy == nil {
		return "", fmt.Errorf("policy 不能为空")
	}
	if policy.Hash != "" {
		return policy.Hash, nil
	}
	return identity.HashCanonical(policy.canonical())
}

func (d Decision) Severity() int {
	switch d {
	case DecisionReject:
		return 2
	case DecisionHold:
		return 1
	default:
		return 0
	}
}

// Policy is the normalized ReleasePolicy document.
type Policy struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   PolicyMetadata `json:"metadata"`
	Spec       PolicySpec     `json:"spec"`
	Hash       string         `json:"-"`
}

// PolicyMetadata identifies a versioned policy.
type PolicyMetadata struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version" yaml:"version"`
}

// PolicySpec holds ordered rules and the default decision.
type PolicySpec struct {
	Rules   []Rule   `json:"rules" yaml:"rules"`
	Default Decision `json:"default" yaml:"default"`
}

// Rule is a single CEL-gated decision.
type Rule struct {
	ID       string   `json:"id" yaml:"id"`
	Priority int      `json:"priority" yaml:"priority"`
	Decision Decision `json:"decision" yaml:"decision"`
	When     string   `json:"when" yaml:"when"`
	Reason   string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

var kebabName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ParsePolicyYAML parses and statically validates a ReleasePolicy document.
func ParsePolicyYAML(raw []byte) (*Policy, []validation.Diagnostic) {
	var wire struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name    string `yaml:"name"`
			Version any    `yaml:"version"`
		} `yaml:"metadata"`
		Spec struct {
			Rules   []Rule `yaml:"rules"`
			Default string `yaml:"default"`
		} `yaml:"spec"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&wire); err != nil {
		return nil, []validation.Diagnostic{{
			Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "Policy YAML 无法解析。",
		}}
	}

	var diags []validation.Diagnostic
	if wire.APIVersion != policyAPIVersion {
		diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "apiVersion", Message: "apiVersion 必须为 skillgate.dev/v1alpha1。"})
	}
	if wire.Kind != policyKind {
		diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "kind", Message: "kind 必须为 ReleasePolicy。"})
	}
	if !kebabName.MatchString(wire.Metadata.Name) {
		diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "metadata.name", Message: "metadata.name 必须为非空 kebab-case。"})
	}
	version := stringifyVersion(wire.Metadata.Version)
	if version == "" {
		diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "metadata.version", Message: "metadata.version 不能为空。"})
	}

	if secret := findSecretField(map[string]any{
		"metadata": map[string]any{"name": wire.Metadata.Name, "version": version},
		"spec":     map[string]any{"rules": wire.Spec.Rules, "default": wire.Spec.Default},
	}, ""); secret != "" {
		diags = append(diags, validation.Diagnostic{Code: "SECRET_FIELD_PRESENT", Severity: "error", Path: "spec." + secret, Message: "Policy 包含 Secret 字段。"})
	}

	seenID := map[string]struct{}{}
	seenPriority := map[int]string{}
	rules := make([]Rule, 0, len(wire.Spec.Rules))
	for i, rule := range wire.Spec.Rules {
		path := fmt.Sprintf("spec.rules[%d]", i)
		if rule.ID == "" || !kebabName.MatchString(rule.ID) {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".id", Message: "rule id 必须为非空 kebab-case。"})
		}
		if _, dup := seenID[rule.ID]; dup && rule.ID != "" {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".id", Message: "rule id 重复。"})
		}
		seenID[rule.ID] = struct{}{}
		if existing, ok := seenPriority[rule.Priority]; ok {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".priority", Message: fmt.Sprintf("priority %d 与 rule %s 冲突。", rule.Priority, existing)})
		}
		if rule.ID != "" {
			seenPriority[rule.Priority] = rule.ID
		}
		if !rule.Decision.Valid() {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".decision", Message: "decision 必须为 PROMOTE、HOLD 或 REJECT。"})
		}
		when := strings.TrimSpace(rule.When)
		if when == "" {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".when", Message: "when 不能为空。"})
		}
		if len(when) > maxExpressionBytes {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".when", Message: fmt.Sprintf("when 超过 %d 字节。", maxExpressionBytes)})
		}
		if fn := bannedFunction(when); fn != "" {
			diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: path + ".when", Message: "CEL 不允许 Network/File Function：" + fn})
		}
		rule.When = when
		rules = append(rules, rule)
	}

	def := Decision(strings.TrimSpace(wire.Spec.Default))
	if def == "" {
		def = DecisionHold
	}
	if !def.Valid() {
		diags = append(diags, validation.Diagnostic{Code: "INVALID_ARGUMENT", Severity: "error", Path: "spec.default", Message: "default 必须为 PROMOTE、HOLD 或 REJECT。"})
	}

	if len(diags) > 0 {
		return nil, diags
	}

	policy := &Policy{
		APIVersion: policyAPIVersion,
		Kind:       policyKind,
		Metadata:   PolicyMetadata{Name: wire.Metadata.Name, Version: version},
		Spec:       PolicySpec{Rules: rules, Default: def},
	}
	hash, err := identity.HashCanonical(policy.canonical())
	if err != nil {
		return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "无法计算 policy hash。"}}
	}
	policy.Hash = hash
	return policy, nil
}

func (p *Policy) canonical() map[string]any {
	rules := make([]any, 0, len(p.Spec.Rules))
	for _, rule := range p.Spec.Rules {
		item := map[string]any{
			"id":       rule.ID,
			"priority": rule.Priority,
			"decision": string(rule.Decision),
			"when":     rule.When,
		}
		if rule.Reason != "" {
			item["reason"] = rule.Reason
		}
		rules = append(rules, item)
	}
	return map[string]any{
		"apiVersion": p.APIVersion,
		"kind":       p.Kind,
		"metadata":   map[string]any{"name": p.Metadata.Name, "version": p.Metadata.Version},
		"spec":       map[string]any{"rules": rules, "default": string(p.Spec.Default)},
	}
}

func stringifyVersion(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case float64:
		if v == float64(int(v)) {
			return fmt.Sprintf("%d", int(v))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

var bannedFunctions = []string{
	"open", "read", "write", "file", "http", "https", "tcp", "udp",
	"socket", "dial", "fetch", "exec", "system", "popen",
}

func bannedFunction(expr string) string {
	lower := strings.ToLower(expr)
	for _, fn := range bannedFunctions {
		if strings.Contains(lower, fn+"(") {
			return fn
		}
	}
	return ""
}

func findSecretField(value any, prefix string) string {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if looksSecret(key) {
				return path
			}
			if found := findSecretField(child, path); found != "" {
				return found
			}
		}
	case []any:
		for i, child := range v {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			if found := findSecretField(child, path); found != "" {
				return found
			}
		}
	case []Rule:
		for i, rule := range v {
			if looksSecret(rule.ID) || looksSecret(rule.When) || looksSecret(rule.Reason) {
				return fmt.Sprintf("%s[%d]", prefix, i)
			}
		}
	}
	return ""
}

func looksSecret(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "token") {
		if strings.Contains(lower, "token_delta") {
			return false
		}
		for _, r := range name {
			if unicode.IsSpace(r) {
				return false
			}
		}
		return true
	}
	return false
}
