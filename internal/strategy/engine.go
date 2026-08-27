package strategy

import (
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/Lin-xun1113/SkillGate/internal/validation"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

// Context is the frozen CEL input. Only these declared fields are visible.
type Context struct {
	Utility     UtilityContext     `json:"utility"`
	Routing     RoutingContext     `json:"routing"`
	Reliability ReliabilityContext `json:"reliability"`
	Cost        CostContext        `json:"cost"`
	Security    SecurityContext    `json:"security"`
	Evidence    EvidenceContext    `json:"evidence"`
	Experiment  ExperimentContext  `json:"experiment"`
}

type UtilityContext struct {
	Lift       float64 `json:"lift"`
	CILower    float64 `json:"ci_lower"`
	CIUpper    float64 `json:"ci_upper"`
	ValidCases int     `json:"valid_cases"`
}

type RoutingContext struct {
	Recall      float64 `json:"recall"`
	Specificity float64 `json:"specificity"`
}

type ReliabilityContext struct {
	PassAt3 float64 `json:"pass_at_3"`
}

type CostContext struct {
	TokenDeltaRatio float64 `json:"token_delta_ratio"`
}

type SecurityContext struct {
	Critical          int `json:"critical"`
	High              int `json:"high"`
	ConfirmedExploits int `json:"confirmed_exploits"`
}

type EvidenceContext struct {
	Complete             bool `json:"complete"`
	IdentityValid        bool `json:"identity_valid"`
	TriggerEvaluated     bool `json:"trigger_evaluated"`
	ReliabilityEvaluated bool `json:"reliability_evaluated"`
	SecurityEvaluated    bool `json:"security_evaluated"`
}

type ExperimentContext struct {
	PairingValid     bool `json:"pairing_valid"`
	InvalidPairs     int  `json:"invalid_pairs"`
	IncompleteTrials int  `json:"incomplete_trials"`
}

// EvaluatedRule records whether a compiled rule matched.
type EvaluatedRule struct {
	ID      string `json:"id"`
	Matched bool   `json:"matched"`
}

// FailedCondition records an expression that blocked promotion.
type FailedCondition struct {
	RuleID string `json:"rule_id,omitempty"`
	When   string `json:"when,omitempty"`
	Reason string `json:"reason,omitempty"`
	Actual string `json:"actual,omitempty"`
}

// Trace is the explainable evaluation result.
type Trace struct {
	Result           Decision          `json:"result"`
	PolicyID         string            `json:"policy_id"`
	PolicyVersion    string            `json:"policy_version"`
	PolicyHash       string            `json:"policy_hash"`
	MatchedRules     []string          `json:"matched_rules"`
	EvaluatedRules   []EvaluatedRule   `json:"evaluated_rules"`
	FailedConditions []FailedCondition `json:"failed_conditions"`
	Explanation      string            `json:"explanation"`
	Error            string            `json:"error,omitempty"`
}

// CompiledPolicy is a type-checked, cached CEL program set.
type CompiledPolicy struct {
	Policy   *Policy
	env      *cel.Env
	programs []compiledRule
}

type compiledRule struct {
	Rule    Rule
	Program cel.Program
}

var (
	envOnce      sync.Once
	sharedEnv    *cel.Env
	envErr       error
	cacheMu      sync.RWMutex
	programCache = map[string]*CompiledPolicy{}
)

func policyEnv() (*cel.Env, error) {
	envOnce.Do(func() {
		sharedEnv, envErr = cel.NewEnv(
			cel.Variable("utility", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("routing", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("reliability", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("cost", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("security", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("evidence", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("experiment", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return sharedEnv, envErr
}

// Compile type-checks every rule and caches the result by Policy Hash.
func Compile(policy *Policy) (*CompiledPolicy, []validation.Diagnostic) {
	if policy == nil {
		return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "policy 不能为空。"}}
	}
	if policy.Hash == "" {
		hash, err := HashPolicy(policy)
		if err != nil {
			return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "无法计算 policy hash。"}}
		}
		policy.Hash = hash
	}
	cacheMu.RLock()
	if cached, ok := programCache[policy.Hash]; ok {
		cacheMu.RUnlock()
		return cached, nil
	}
	cacheMu.RUnlock()

	env, err := policyEnv()
	if err != nil {
		return nil, []validation.Diagnostic{{Code: "INVALID_ARGUMENT", Severity: "error", Path: "", Message: "无法创建 CEL Environment：" + err.Error()}}
	}

	var diags []validation.Diagnostic
	compiled := make([]compiledRule, 0, len(policy.Spec.Rules))
	for i, rule := range policy.Spec.Rules {
		if field := unknownContextField(rule.When); field != "" {
			diags = append(diags, validation.Diagnostic{
				Code:     "INVALID_ARGUMENT",
				Severity: "error",
				Path:     fmt.Sprintf("spec.rules[%d].when", i),
				Message:  "CEL 包含未声明字段：" + field,
			})
			continue
		}
		ast, iss := env.Compile(rule.When)
		if iss != nil && iss.Err() != nil {
			diags = append(diags, validation.Diagnostic{
				Code:     "INVALID_ARGUMENT",
				Severity: "error",
				Path:     fmt.Sprintf("spec.rules[%d].when", i),
				Message:  "CEL 编译失败：" + iss.Err().Error(),
			})
			continue
		}
		if ast.OutputType() != nil && ast.OutputType() != cel.BoolType {
			diags = append(diags, validation.Diagnostic{
				Code:     "INVALID_ARGUMENT",
				Severity: "error",
				Path:     fmt.Sprintf("spec.rules[%d].when", i),
				Message:  "CEL when 必须求值为 bool。",
			})
			continue
		}
		program, err := env.Program(ast)
		if err != nil {
			diags = append(diags, validation.Diagnostic{
				Code:     "INVALID_ARGUMENT",
				Severity: "error",
				Path:     fmt.Sprintf("spec.rules[%d].when", i),
				Message:  "CEL Program 创建失败：" + err.Error(),
			})
			continue
		}
		compiled = append(compiled, compiledRule{Rule: rule, Program: program})
	}
	if len(diags) > 0 {
		return nil, diags
	}

	sort.SliceStable(compiled, func(i, j int) bool {
		return compiled[i].Rule.Priority > compiled[j].Rule.Priority
	})
	out := &CompiledPolicy{Policy: policy, env: env, programs: compiled}
	cacheMu.Lock()
	programCache[policy.Hash] = out
	cacheMu.Unlock()
	return out, nil
}

// Evaluate runs rules in descending priority. The first true when wins.
func (c *CompiledPolicy) Evaluate(ctx Context) Trace {
	trace := Trace{
		Result:           c.Policy.Spec.Default,
		PolicyID:         c.Policy.Metadata.Name,
		PolicyVersion:    c.Policy.Metadata.Version,
		PolicyHash:       c.Policy.Hash,
		MatchedRules:     []string{},
		EvaluatedRules:   []EvaluatedRule{},
		FailedConditions: []FailedCondition{},
		Explanation:      fmt.Sprintf("no rule matched; default %s", c.Policy.Spec.Default),
	}

	activation := contextMap(ctx)
	for _, item := range c.programs {
		out, _, err := item.Program.Eval(activation)
		if err != nil {
			trace.Result = DecisionHold
			trace.Error = err.Error()
			trace.Explanation = "policy evaluation error; fail closed to HOLD"
			trace.FailedConditions = append(trace.FailedConditions, FailedCondition{
				RuleID: item.Rule.ID,
				When:   item.Rule.When,
				Reason: "evaluation_error",
				Actual: err.Error(),
			})
			return trace
		}
		matched := isTrue(out)
		trace.EvaluatedRules = append(trace.EvaluatedRules, EvaluatedRule{ID: item.Rule.ID, Matched: matched})
		if !matched {
			continue
		}
		trace.MatchedRules = []string{item.Rule.ID}
		trace.Result = item.Rule.Decision
		if item.Rule.Reason != "" {
			trace.Explanation = item.Rule.Reason
		} else {
			trace.Explanation = fmt.Sprintf("rule %s matched", item.Rule.ID)
		}
		if item.Rule.Decision != DecisionPromote {
			reason := item.Rule.Reason
			if reason == "" {
				reason = "rule " + item.Rule.ID
			}
			trace.FailedConditions = append(trace.FailedConditions, FailedCondition{
				RuleID: item.Rule.ID,
				When:   item.Rule.When,
				Reason: reason,
				Actual: summarizeContext(ctx),
			})
		}
		return trace
	}
	if trace.Result != DecisionPromote {
		trace.FailedConditions = append(trace.FailedConditions, FailedCondition{
			Reason: "default",
			Actual: summarizeContext(ctx),
		})
	}
	return trace
}

func isTrue(v ref.Val) bool {
	if v == nil {
		return false
	}
	b, ok := v.Value().(bool)
	return ok && b
}

func contextMap(ctx Context) map[string]any {
	return map[string]any{
		"utility": map[string]any{
			"lift":        ctx.Utility.Lift,
			"ci_lower":    ctx.Utility.CILower,
			"ci_upper":    ctx.Utility.CIUpper,
			"valid_cases": ctx.Utility.ValidCases,
		},
		"routing": map[string]any{
			"recall":      ctx.Routing.Recall,
			"specificity": ctx.Routing.Specificity,
		},
		"reliability": map[string]any{
			"pass_at_3": ctx.Reliability.PassAt3,
		},
		"cost": map[string]any{
			"token_delta_ratio": ctx.Cost.TokenDeltaRatio,
		},
		"security": map[string]any{
			"critical":           ctx.Security.Critical,
			"high":               ctx.Security.High,
			"confirmed_exploits": ctx.Security.ConfirmedExploits,
		},
		"evidence": map[string]any{
			"complete":              ctx.Evidence.Complete,
			"identity_valid":        ctx.Evidence.IdentityValid,
			"trigger_evaluated":     ctx.Evidence.TriggerEvaluated,
			"reliability_evaluated": ctx.Evidence.ReliabilityEvaluated,
			"security_evaluated":    ctx.Evidence.SecurityEvaluated,
		},
		"experiment": map[string]any{
			"pairing_valid":     ctx.Experiment.PairingValid,
			"incomplete_trials": ctx.Experiment.IncompleteTrials,
		},
	}
}

var declaredContextFields = map[string]struct{}{
	"utility.lift": {}, "utility.ci_lower": {}, "utility.ci_upper": {}, "utility.valid_cases": {},
	"routing.recall": {}, "routing.specificity": {},
	"reliability.pass_at_3":  {},
	"cost.token_delta_ratio": {},
	"security.critical":      {}, "security.high": {}, "security.confirmed_exploits": {},
	"evidence.complete": {}, "evidence.identity_valid": {}, "evidence.trigger_evaluated": {},
	"evidence.reliability_evaluated": {}, "evidence.security_evaluated": {},
	"experiment.pairing_valid": {}, "experiment.incomplete_trials": {},
}

var contextFieldPattern = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\b`)

func unknownContextField(expr string) string {
	for _, match := range contextFieldPattern.FindAllString(expr, -1) {
		if _, ok := declaredContextFields[match]; !ok {
			return match
		}
	}
	return ""
}

func summarizeContext(ctx Context) string {
	return fmt.Sprintf("utility.ci_lower=%.4f routing.recall=%.3f routing.specificity=%.3f reliability.pass_at_3=%.3f cost.token_delta_ratio=%.3f security.critical=%d security.confirmed_exploits=%d evidence.complete=%t",
		ctx.Utility.CILower, ctx.Routing.Recall, ctx.Routing.Specificity, ctx.Reliability.PassAt3, ctx.Cost.TokenDeltaRatio, ctx.Security.Critical, ctx.Security.ConfirmedExploits, ctx.Evidence.Complete)
}

// ResetCache is for tests.
func ResetCache() {
	cacheMu.Lock()
	programCache = map[string]*CompiledPolicy{}
	cacheMu.Unlock()
}
