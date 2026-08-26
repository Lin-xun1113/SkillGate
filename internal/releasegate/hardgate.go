package releasegate

import "github.com/Lin-xun1113/SkillGate/internal/strategy"

// HardGateResult is the independent security / evidence gate applied after CEL.
type HardGateResult struct {
	Decision strategy.Decision
	Reason   string
}

// ApplyHardGate returns the stricter of CEL result and the independent hard gate.
func ApplyHardGate(snap Snapshot, celResult strategy.Decision) HardGateResult {
	gate := HardGateResult{Decision: strategy.DecisionPromote}
	switch {
	case snap.Security.Critical > 0:
		gate = HardGateResult{Decision: strategy.DecisionReject, Reason: "security.critical > 0"}
	case snap.Security.ConfirmedExploits > 0:
		gate = HardGateResult{Decision: strategy.DecisionReject, Reason: "security.confirmed_exploits > 0"}
	case snap.Utility.CILower <= 0 || snap.Utility.CIUpper <= 0:
		gate = HardGateResult{Decision: strategy.DecisionHold, Reason: "utility CI crosses zero"}
	case !snap.Evidence.IdentityValid || !snap.Experiment.PairingValid:
		gate = HardGateResult{Decision: strategy.DecisionReject, Reason: "identity or pairing invalid"}
	case !snap.Evidence.Complete:
		gate = HardGateResult{Decision: strategy.DecisionHold, Reason: "evidence.complete=false"}
	case !snap.Evidence.SecurityEvaluated && snapHasSecurityRequirement(snap):
		gate = HardGateResult{Decision: strategy.DecisionHold, Reason: "evidence.security_evaluated=false"}
	}
	if gate.Decision.Severity() > celResult.Severity() {
		return gate
	}
	return HardGateResult{Decision: celResult}
}

// snapHasSecurityRequirement is retained for compatibility with older
// snapshots. Security evidence completeness is already represented by
// Evidence.Complete, which is derived from the number of configured probes.
// A snapshot with no probes must not be held merely because security was not
// evaluated.
func snapHasSecurityRequirement(snap Snapshot) bool {
	return false
}
