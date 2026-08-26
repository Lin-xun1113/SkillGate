package strategy

import (
	"os"
	"strings"
	"testing"
)

func TestParseAndCompileConservativePolicy(t *testing.T) {
	ResetCache()
	raw, err := os.ReadFile("../../policies/conservative-release.yaml")
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	policy, diags := ParsePolicyYAML(raw)
	if len(diags) > 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	compiled, diags := Compile(policy)
	if len(diags) > 0 {
		t.Fatalf("compile diags: %+v", diags)
	}
	again, diags := Compile(policy)
	if len(diags) > 0 {
		t.Fatalf("second compile diags: %+v", diags)
	}
	if compiled != again {
		t.Fatal("expected compiled policy to be reused by hash cache")
	}
}

func TestParseRejectsDuplicatePriority(t *testing.T) {
	raw := []byte(`
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: bad-priority
  version: 1
spec:
  rules:
    - id: one
      priority: 10
      decision: HOLD
      when: "utility.lift > 0"
    - id: two
      priority: 10
      decision: REJECT
      when: "security.critical > 0"
  default: HOLD
`)
	_, diags := ParsePolicyYAML(raw)
	if len(diags) == 0 {
		t.Fatal("expected duplicate priority to be rejected")
	}
}

func TestParseRejectsBannedFunction(t *testing.T) {
	raw := []byte(`
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: bad-fn
  version: 1
spec:
  rules:
    - id: net
      priority: 10
      decision: REJECT
      when: "http('https://example.com')"
  default: HOLD
`)
	_, diags := ParsePolicyYAML(raw)
	if len(diags) == 0 {
		t.Fatal("expected banned function to be rejected")
	}
	if !strings.Contains(diags[0].Message, "Network/File") {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestParseRejectsOverlongExpression(t *testing.T) {
	when := strings.Repeat("utility.lift > 0 && ", 200) + "true"
	raw := []byte(`
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: too-long
  version: 1
spec:
  rules:
    - id: long
      priority: 10
      decision: HOLD
      when: "` + when + `"
  default: HOLD
`)
	_, diags := ParsePolicyYAML(raw)
	if len(diags) == 0 {
		t.Fatal("expected overlong expression to be rejected")
	}
}

func TestEvaluatePromote(t *testing.T) {
	ResetCache()
	p := mustPolicy(t, `
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: conservative-skill-promotion
  version: 1
spec:
  rules:
    - id: promote
      priority: 100
      decision: PROMOTE
      when: "utility.ci_lower > 0 && routing.recall >= 0.9 && routing.specificity >= 0.85 && reliability.pass_at_3 >= 0.8 && cost.token_delta_ratio <= 0.3"
      reason: "gates passed"
  default: HOLD
`)
	compiled, diags := Compile(p)
	if len(diags) > 0 {
		t.Fatalf("compile: %+v", diags)
	}
	trace := compiled.Evaluate(promoteContext())
	if trace.Result != DecisionPromote {
		t.Fatalf("expected PROMOTE, got %s (%s)", trace.Result, trace.Explanation)
	}
	if len(trace.MatchedRules) != 1 || trace.MatchedRules[0] != "promote" {
		t.Fatalf("matched_rules = %#v", trace.MatchedRules)
	}
	if len(trace.FailedConditions) != 0 {
		t.Fatalf("unexpected failed conditions: %+v", trace.FailedConditions)
	}
}

func TestEvaluateRejectCritical(t *testing.T) {
	ResetCache()
	p := mustPolicy(t, `
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: conservative-skill-promotion
  version: 1
spec:
  rules:
    - id: reject-critical
      priority: 1000
      decision: REJECT
      when: "security.critical > 0"
      reason: "critical security"
    - id: promote
      priority: 100
      decision: PROMOTE
      when: "utility.ci_lower > 0"
  default: HOLD
`)
	compiled, diags := Compile(p)
	if len(diags) > 0 {
		t.Fatalf("compile: %+v", diags)
	}
	ctx := promoteContext()
	ctx.Security.Critical = 1
	trace := compiled.Evaluate(ctx)
	if trace.Result != DecisionReject {
		t.Fatalf("expected REJECT, got %s", trace.Result)
	}
	if len(trace.EvaluatedRules) == 0 || !trace.EvaluatedRules[0].Matched {
		t.Fatalf("expected first evaluated rule to match, got %+v", trace.EvaluatedRules)
	}
}

func TestUnknownFieldRejectedAtCompile(t *testing.T) {
	ResetCache()
	p := mustPolicy(t, `
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: unknown-field
  version: 1
spec:
  rules:
    - id: bad
      priority: 10
      decision: HOLD
      when: "utility.missing_field > 0"
  default: HOLD
`)
	_, diags := Compile(p)
	if len(diags) == 0 {
		t.Fatal("expected unknown field to fail CEL compile")
	}
}

func mustPolicy(t *testing.T, raw string) *Policy {
	t.Helper()
	p, diags := ParsePolicyYAML([]byte(raw))
	if len(diags) > 0 {
		t.Fatalf("parse: %+v", diags)
	}
	return p
}

func promoteContext() Context {
	return Context{
		Utility:     UtilityContext{Lift: 0.15, CILower: 0.05, CIUpper: 0.25, ValidCases: 10},
		Routing:     RoutingContext{Recall: 0.95, Specificity: 0.90},
		Reliability: ReliabilityContext{PassAt3: 0.91},
		Cost:        CostContext{TokenDeltaRatio: 0.18},
		Security:    SecurityContext{},
		Evidence: EvidenceContext{
			Complete:             true,
			IdentityValid:        true,
			TriggerEvaluated:     true,
			ReliabilityEvaluated: true,
			SecurityEvaluated:    true,
		},
		Experiment: ExperimentContext{PairingValid: true},
	}
}
