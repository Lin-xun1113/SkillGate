package releasegate

import (
	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/metrics"
	"github.com/Lin-xun1113/SkillGate/internal/statistics"
	"github.com/Lin-xun1113/SkillGate/internal/strategy"
)

const snapshotSchema = "skillgate.metric-snapshot.v1"

// Snapshot is the immutable metric context used for a release decision.
type Snapshot struct {
	Schema       string                      `json:"schema"`
	ExperimentID string                      `json:"experiment_id"`
	PolicyHash   string                      `json:"policy_hash,omitempty"`
	Utility      strategy.UtilityContext     `json:"utility"`
	Routing      strategy.RoutingContext     `json:"routing"`
	Reliability  strategy.ReliabilityContext `json:"reliability"`
	Cost         strategy.CostContext        `json:"cost"`
	Security     strategy.SecurityContext    `json:"security"`
	Evidence     strategy.EvidenceContext    `json:"evidence"`
	Experiment   strategy.ExperimentContext  `json:"experiment"`
	Hash         string                      `json:"-"`
	ReportPath   string                      `json:"report_path,omitempty"`
}

// SnapshotInput is the raw aggregation input used to build a Snapshot.
type SnapshotInput struct {
	ExperimentID     string
	PolicyHash       string
	Lift             float64
	CILower          float64
	CIUpper          float64
	ValidCases       int
	CIAvailable      bool
	Trigger          metrics.TriggerResult
	Security         metrics.SecurityResult
	PassAt3          float64
	PassAt3Available bool
	TokenDeltaRatio  float64
	IdentityValid    bool
	PairingValid     bool
	InvalidPairCount int
	IncompleteTrials int
}

// BuildSnapshot constructs a frozen metric snapshot and its canonical hash.
func BuildSnapshot(in SnapshotInput) (Snapshot, error) {
	snap := Snapshot{
		Schema:       snapshotSchema,
		ExperimentID: in.ExperimentID,
		PolicyHash:   in.PolicyHash,
		Utility: strategy.UtilityContext{
			Lift:       in.Lift,
			CILower:    in.CILower,
			CIUpper:    in.CIUpper,
			ValidCases: in.ValidCases,
		},
		Routing: strategy.RoutingContext{
			Recall:      in.Trigger.Recall,
			Specificity: in.Trigger.Specificity,
		},
		Reliability: strategy.ReliabilityContext{PassAt3: in.PassAt3},
		Cost:        strategy.CostContext{TokenDeltaRatio: in.TokenDeltaRatio},
		Security: strategy.SecurityContext{
			Critical:          in.Security.Critical,
			High:              in.Security.High,
			ConfirmedExploits: in.Security.ConfirmedExploits,
		},
		Evidence: strategy.EvidenceContext{
			IdentityValid:        in.IdentityValid,
			TriggerEvaluated:     in.Trigger.Evaluated,
			ReliabilityEvaluated: in.PassAt3Available,
			SecurityEvaluated:    in.Security.Evaluated,
		},
		Experiment: strategy.ExperimentContext{
			PairingValid:     in.PairingValid,
			InvalidPairs:     in.InvalidPairCount,
			IncompleteTrials: in.IncompleteTrials,
		},
	}
	if !in.CIAvailable || in.ValidCases < 2 {
		// Spec §4: fewer than 2 valid pairs is an uncertain interval crossing 0.
		snap.Utility.CILower = -1
		snap.Utility.CIUpper = 1
	}
	incomplete := in.IncompleteTrials > 0 || in.Trigger.IncompleteCases > 0 || in.Security.MissingEvidence > 0
	snap.Evidence.Complete = in.IdentityValid && in.PairingValid && !incomplete &&
		(in.Security.ProbeCases == 0 || in.Security.Evaluated)
	hash, err := identity.HashCanonical(snap.Canonical())
	if err != nil {
		return Snapshot{}, err
	}
	snap.Hash = hash
	return snap, nil
}

// Canonical is the wallclock-free payload hashed as snapshot_hash.
func (s Snapshot) Canonical() map[string]any {
	return map[string]any{
		"schema":        s.Schema,
		"experiment_id": s.ExperimentID,
		"policy_hash":   s.PolicyHash,
		"utility": map[string]any{
			"lift":        s.Utility.Lift,
			"ci_lower":    s.Utility.CILower,
			"ci_upper":    s.Utility.CIUpper,
			"valid_cases": s.Utility.ValidCases,
		},
		"routing": map[string]any{
			"recall":      s.Routing.Recall,
			"specificity": s.Routing.Specificity,
		},
		"reliability": map[string]any{
			"pass_at_3": s.Reliability.PassAt3,
		},
		"cost": map[string]any{
			"token_delta_ratio": s.Cost.TokenDeltaRatio,
		},
		"security": map[string]any{
			"critical":           s.Security.Critical,
			"high":               s.Security.High,
			"confirmed_exploits": s.Security.ConfirmedExploits,
		},
		"evidence": map[string]any{
			"complete":              s.Evidence.Complete,
			"identity_valid":        s.Evidence.IdentityValid,
			"trigger_evaluated":     s.Evidence.TriggerEvaluated,
			"reliability_evaluated": s.Evidence.ReliabilityEvaluated,
			"security_evaluated":    s.Evidence.SecurityEvaluated,
		},
		"experiment": map[string]any{
			"pairing_valid":     s.Experiment.PairingValid,
			"incomplete_trials": s.Experiment.IncompleteTrials,
		},
	}
}

// Context converts the snapshot into the CEL evaluation context.
func (s Snapshot) Context() strategy.Context {
	return strategy.Context{
		Utility:     s.Utility,
		Routing:     s.Routing,
		Reliability: s.Reliability,
		Cost:        s.Cost,
		Security:    s.Security,
		Evidence:    s.Evidence,
		Experiment:  s.Experiment,
	}
}

// UtilityFromBootstrap maps a bootstrap result onto snapshot utility fields.
func UtilityFromBootstrap(result *statistics.BootstrapResult) (lift, lower, upper float64, valid int, ok bool) {
	if result == nil {
		return 0, -1, 1, 0, false
	}
	return result.MeanEstimate, result.CILower, result.CIUpper, result.NCases, result.NCases >= 2 && result.Method != "insufficient_data"
}
