package releasegate

import (
	"fmt"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/strategy"
)

// Decision is the persisted, explainable release outcome.
type Decision struct {
	DecisionID       string                     `json:"decision_id"`
	ExperimentID     string                     `json:"experiment_id"`
	Result           strategy.Decision          `json:"result"`
	PolicyID         string                     `json:"policy_id"`
	PolicyVersion    string                     `json:"policy_version"`
	PolicyHash       string                     `json:"policy_hash"`
	SnapshotHash     string                     `json:"snapshot_hash"`
	MatchedRules     []string                   `json:"matched_rules"`
	EvaluatedRules   []strategy.EvaluatedRule   `json:"evaluated_rules"`
	FailedConditions []strategy.FailedCondition `json:"failed_conditions"`
	HardGateOverride *string                    `json:"hard_gate_override"`
	Explanation      string                     `json:"explanation"`
	CreatedAt        time.Time                  `json:"created_at"`
}

// Evaluate compiles the policy, evaluates it against the frozen snapshot, then
// applies Hard Gate. Policy errors fail closed to HOLD (or REJECT if Hard Gate
// is stricter). The result never silently becomes PROMOTE.
func Evaluate(policy *strategy.Policy, snap Snapshot) Decision {
	created := time.Now().UTC()
	base := Decision{
		ExperimentID:     snap.ExperimentID,
		SnapshotHash:     snap.Hash,
		MatchedRules:     []string{},
		EvaluatedRules:   []strategy.EvaluatedRule{},
		FailedConditions: []strategy.FailedCondition{},
		CreatedAt:        created,
	}
	if policy != nil {
		base.PolicyID = policy.Metadata.Name
		base.PolicyVersion = policy.Metadata.Version
		base.PolicyHash = policy.Hash
	}

	if policy == nil {
		base.Result = strategy.DecisionHold
		base.Explanation = "policy missing; fail closed to HOLD"
		base.FailedConditions = []strategy.FailedCondition{{Reason: "policy_missing"}}
		base.DecisionID = decisionID(base)
		return base
	}

	compiled, diags := strategy.Compile(policy)
	if len(diags) > 0 {
		base.Result = strategy.DecisionHold
		base.Explanation = "policy compile error; fail closed to HOLD"
		base.FailedConditions = []strategy.FailedCondition{{
			Reason: "compile_error",
			Actual: diags[0].Message,
		}}
		gate := ApplyHardGate(snap, base.Result)
		if gate.Decision.Severity() > base.Result.Severity() {
			reason := gate.Reason
			base.HardGateOverride = &reason
			base.Result = gate.Decision
			base.Explanation = "hard_gate_override: " + gate.Reason
		}
		base.DecisionID = decisionID(base)
		return base
	}

	trace := compiled.Evaluate(snap.Context())
	base.Result = trace.Result
	base.MatchedRules = append([]string{}, trace.MatchedRules...)
	base.EvaluatedRules = append([]strategy.EvaluatedRule{}, trace.EvaluatedRules...)
	base.FailedConditions = append([]strategy.FailedCondition{}, trace.FailedConditions...)
	base.Explanation = trace.Explanation
	if trace.Error != "" {
		base.Result = strategy.DecisionHold
		base.Explanation = "policy evaluation error; fail closed to HOLD"
		base.FailedConditions = append(base.FailedConditions, strategy.FailedCondition{
			Reason: "evaluation_error",
			Actual: trace.Error,
		})
	}

	gate := ApplyHardGate(snap, base.Result)
	if gate.Decision.Severity() > base.Result.Severity() {
		reason := gate.Reason
		base.HardGateOverride = &reason
		base.Result = gate.Decision
		if gate.Reason != "" {
			base.FailedConditions = append(base.FailedConditions, strategy.FailedCondition{
				Reason: "hard_gate_override",
				Actual: gate.Reason,
			})
			base.Explanation = fmt.Sprintf("hard_gate_override: %s (cel=%s)", gate.Reason, trace.Result)
		}
	}
	base.DecisionID = decisionID(base)
	return base
}

func decisionID(d Decision) string {
	hash, err := identity.HashCanonical(map[string]any{
		"experiment_id": d.ExperimentID,
		"policy_hash":   d.PolicyHash,
		"snapshot_hash": d.SnapshotHash,
		"result":        string(d.Result),
	})
	if err != nil {
		return "dec_unknown"
	}
	return "dec_" + hash[len("sha256:"):]
}

// ReportDecision is the optional report.decision section.
type ReportDecision struct {
	Result           string                     `json:"result"`
	PolicyID         string                     `json:"policy_id"`
	PolicyVersion    string                     `json:"policy_version"`
	PolicyHash       string                     `json:"policy_hash"`
	SnapshotHash     string                     `json:"snapshot_hash"`
	MatchedRules     []string                   `json:"matched_rules"`
	EvaluatedRules   []strategy.EvaluatedRule   `json:"evaluated_rules"`
	FailedConditions []strategy.FailedCondition `json:"failed_conditions"`
	HardGateOverride *string                    `json:"hard_gate_override"`
	Explanation      string                     `json:"explanation"`
}

// ToReportDecision projects a Decision onto the report schema.
func ToReportDecision(d Decision) *ReportDecision {
	if d.DecisionID == "" && d.Result == "" {
		return nil
	}
	return &ReportDecision{
		Result:           string(d.Result),
		PolicyID:         d.PolicyID,
		PolicyVersion:    d.PolicyVersion,
		PolicyHash:       d.PolicyHash,
		SnapshotHash:     d.SnapshotHash,
		MatchedRules:     d.MatchedRules,
		EvaluatedRules:   d.EvaluatedRules,
		FailedConditions: d.FailedConditions,
		HardGateOverride: d.HardGateOverride,
		Explanation:      d.Explanation,
	}
}
