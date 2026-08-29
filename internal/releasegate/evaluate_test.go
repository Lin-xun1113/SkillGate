package releasegate

import (
	"os"
	"strings"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/metrics"
	"github.com/Lin-xun1113/SkillGate/internal/strategy"
)

func TestSnapshotHashStable(t *testing.T) {
	in := promoteSnapshotInput()
	first, err := BuildSnapshot(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSnapshot(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash == "" || first.Hash != second.Hash {
		t.Fatalf("snapshot hash unstable: %q vs %q", first.Hash, second.Hash)
	}
}

func TestEvaluatePromote(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	snap := mustSnap(t, promoteSnapshotInput())
	got := Evaluate(policy, snap)
	if got.Result != strategy.DecisionPromote {
		t.Fatalf("got %s (%s)", got.Result, got.Explanation)
	}
	if got.PolicyHash == "" || got.SnapshotHash == "" {
		t.Fatal("trace missing hashes")
	}
	if len(got.MatchedRules) == 0 || got.MatchedRules[0] != "promote" {
		t.Fatalf("matched=%v", got.MatchedRules)
	}
	if len(got.EvaluatedRules) == 0 {
		t.Fatal("evaluated rules missing")
	}
	if got.Actor == "" {
		t.Fatal("actor missing")
	}
	if got.Actor != "grading-service" {
		t.Fatalf("expected actor=grading-service, got %s", got.Actor)
	}
	if len(got.EvidenceLinks) == 0 {
		t.Fatal("evidence links missing")
	}
	hasSnapshot := false
	hasExperiment := false
	for _, link := range got.EvidenceLinks {
		if len(link) > 9 && link[:9] == "snapshot:" {
			hasSnapshot = true
		}
		if len(link) > 11 && link[:11] == "experiment:" {
			hasExperiment = true
		}
	}
	if !hasSnapshot {
		t.Fatal("evidence links missing snapshot reference")
	}
	if !hasExperiment {
		t.Fatal("evidence links missing experiment reference")
	}
}

func TestEvaluateRejectCriticalDespitePositiveLift(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	in := promoteSnapshotInput()
	in.Security = metrics.SecurityResult{Critical: 1, Evaluated: true, ProbeCases: 1}
	snap := mustSnap(t, in)
	got := Evaluate(policy, snap)
	if got.Result != strategy.DecisionReject {
		t.Fatalf("expected REJECT, got %s", got.Result)
	}
}

func TestEvaluateHoldIncompleteEvidence(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	in := promoteSnapshotInput()
	in.Security = metrics.SecurityResult{Evaluated: false, MissingEvidence: 1, ProbeCases: 1}
	snap := mustSnap(t, in)
	if snap.Evidence.Complete {
		t.Fatal("expected incomplete evidence")
	}
	got := Evaluate(policy, snap)
	if got.Result == strategy.DecisionPromote {
		t.Fatalf("incomplete evidence must not PROMOTE: %+v", got)
	}
	if got.Result != strategy.DecisionHold && got.Result != strategy.DecisionReject {
		t.Fatalf("expected HOLD or REJECT, got %s", got.Result)
	}
}

func TestEvaluateHoldWhenCICrossesZero(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	in := promoteSnapshotInput()
	in.CIAvailable = true
	in.CILower = -0.02
	in.CIUpper = 0.10
	in.Lift = 0.04
	snap := mustSnap(t, in)
	got := Evaluate(policy, snap)
	if got.Result == strategy.DecisionPromote {
		t.Fatalf("CI crossing 0 must not PROMOTE: %+v", got)
	}
}

func TestEvaluateIdempotentOnSameSnapshot(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	snap := mustSnap(t, promoteSnapshotInput())
	first := Evaluate(policy, snap)
	second := Evaluate(policy, snap)
	if first.Result != second.Result || first.SnapshotHash != second.SnapshotHash || first.PolicyHash != second.PolicyHash {
		t.Fatalf("repeat evaluation drifted: %+v vs %+v", first, second)
	}
	if first.DecisionID != second.DecisionID {
		t.Fatalf("decision id drifted: %s vs %s", first.DecisionID, second.DecisionID)
	}
}

func promoteSnapshotInput() SnapshotInput {
	return SnapshotInput{
		ExperimentID: "exp-promote",
		PolicyHash:   "sha256:policy",
		Lift:         0.15,
		CILower:      0.05,
		CIUpper:      0.25,
		ValidCases:   10,
		CIAvailable:  true,
		Trigger: metrics.TriggerResult{
			Recall: 0.95, Specificity: 0.90, Evaluated: true, PositiveCases: 4, NegativeCases: 4,
		},
		Security:         metrics.SecurityResult{Evaluated: true, ProbeCases: 1},
		PassAt3:          0.91,
		PassAt3Available: true,
		TokenDeltaRatio:  0.18,
		IdentityValid:    true,
		PairingValid:     true,
	}
}

func mustSnap(t *testing.T, in SnapshotInput) Snapshot {
	t.Helper()
	snap, err := BuildSnapshot(in)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func mustConservative(t *testing.T) *strategy.Policy {
	t.Helper()
	raw, err := os.ReadFile("../../policies/conservative-release.yaml")
	if err != nil {
		t.Fatal(err)
	}
	policy, diags := strategy.ParsePolicyYAML(raw)
	if len(diags) > 0 {
		t.Fatalf("parse: %+v", diags)
	}
	return policy
}

func TestDecisionRoundTripWithActorAndEvidence(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	snap := mustSnap(t, promoteSnapshotInput())

	// Create a decision with all fields
	decision := Evaluate(policy, snap)

	// Verify Actor and EvidenceLinks are populated
	if decision.Actor != "grading-service" {
		t.Fatalf("expected actor=grading-service, got %s", decision.Actor)
	}
	if len(decision.EvidenceLinks) == 0 {
		t.Fatal("expected evidence links to be populated")
	}

	// Verify ToReportDecision preserves new fields
	reportDecision := ToReportDecision(decision)
	if reportDecision == nil {
		t.Fatal("ToReportDecision returned nil")
	}
	if reportDecision.Actor != decision.Actor {
		t.Fatalf("ToReportDecision lost actor: expected %s, got %s", decision.Actor, reportDecision.Actor)
	}
	if len(reportDecision.EvidenceLinks) != len(decision.EvidenceLinks) {
		t.Fatalf("ToReportDecision lost evidence links: expected %d, got %d", len(decision.EvidenceLinks), len(reportDecision.EvidenceLinks))
	}
}

func TestEvaluatePolicyHashMismatchFailsClosed(t *testing.T) {
	strategy.ResetCache()
	policy := mustConservative(t)
	snapInput := promoteSnapshotInput()
	snapInput.PolicyHash = "sha256:" + "0" + strings.Repeat("1", 63)
	snap := mustSnap(t, snapInput)
	decision := Evaluate(policy, snap)
	if decision.Result != strategy.DecisionHold {
		t.Fatalf("policy hash mismatch must HOLD, got %s", decision.Result)
	}
	if len(decision.FailedConditions) == 0 || decision.FailedConditions[0].Reason != "policy_hash_mismatch" {
		t.Fatalf("missing policy hash mismatch trace: %+v", decision.FailedConditions)
	}
}
