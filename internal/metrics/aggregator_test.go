package metrics

import (
	"math"
	"testing"
)

func TestAggregateCaseScores(t *testing.T) {
	trials := []TrialResult{
		{TrialID: "t1", CaseID: "case1", Arm: "baseline", RepetitionIndex: 0, AggregatedScore: 0.8},
		{TrialID: "t2", CaseID: "case1", Arm: "baseline", RepetitionIndex: 1, AggregatedScore: 0.7},
		{TrialID: "t3", CaseID: "case1", Arm: "baseline", RepetitionIndex: 2, AggregatedScore: 0.9},
		{TrialID: "t4", CaseID: "case1", Arm: "candidate", RepetitionIndex: 0, AggregatedScore: 0.85},
		{TrialID: "t5", CaseID: "case1", Arm: "candidate", RepetitionIndex: 1, AggregatedScore: 0.95},
		{TrialID: "t6", CaseID: "case1", Arm: "candidate", RepetitionIndex: 2, AggregatedScore: 0.90},
	}

	caseScores, err := AggregateCaseScores(trials)
	if err != nil {
		t.Fatalf("AggregateCaseScores failed: %v", err)
	}

	if len(caseScores) != 2 {
		t.Fatalf("expected 2 case scores, got %d", len(caseScores))
	}

	// Find baseline and candidate scores
	var baselineScore, candidateScore *CaseScore
	for i := range caseScores {
		if caseScores[i].Arm == "baseline" {
			baselineScore = &caseScores[i]
		} else if caseScores[i].Arm == "candidate" {
			candidateScore = &caseScores[i]
		}
	}

	if baselineScore == nil {
		t.Fatal("baseline score not found")
	}
	if candidateScore == nil {
		t.Fatal("candidate score not found")
	}

	// Check baseline
	expectedBaselineMean := 0.8 // (0.8 + 0.7 + 0.9) / 3
	if math.Abs(baselineScore.MeanScore-expectedBaselineMean) > 0.001 {
		t.Errorf("baseline mean = %.2f, want %.2f", baselineScore.MeanScore, expectedBaselineMean)
	}

	if baselineScore.Repetitions != 3 {
		t.Errorf("baseline repetitions = %d, want 3", baselineScore.Repetitions)
	}

	// Check candidate
	expectedCandidateMean := 0.9 // (0.85 + 0.95 + 0.90) / 3
	if math.Abs(candidateScore.MeanScore-expectedCandidateMean) > 0.001 {
		t.Errorf("candidate mean = %.2f, want %.2f", candidateScore.MeanScore, expectedCandidateMean)
	}

	if candidateScore.Repetitions != 3 {
		t.Errorf("candidate repetitions = %d, want 3", candidateScore.Repetitions)
	}
}

func TestPairCases(t *testing.T) {
	baseline := []CaseScore{
		{CaseID: "case1", Arm: "baseline", MeanScore: 0.7, Repetitions: 3},
		{CaseID: "case2", Arm: "baseline", MeanScore: 0.6, Repetitions: 3},
		{CaseID: "case3", Arm: "baseline", MeanScore: 0.8, Repetitions: 2}, // Different repetitions
	}

	candidate := []CaseScore{
		{CaseID: "case1", Arm: "candidate", MeanScore: 0.9, Repetitions: 3},
		{CaseID: "case2", Arm: "candidate", MeanScore: 0.75, Repetitions: 3},
		{CaseID: "case3", Arm: "candidate", MeanScore: 0.95, Repetitions: 3}, // Different repetitions
		{CaseID: "case4", Arm: "candidate", MeanScore: 0.85, Repetitions: 3}, // No baseline
	}

	pairs, err := PairCases(baseline, candidate, "baseline", "candidate")
	if err != nil {
		t.Fatalf("PairCases failed: %v", err)
	}

	if len(pairs) != 4 {
		t.Fatalf("expected 4 pairs, got %d", len(pairs))
	}

	// Check case1 - valid pair
	case1Pair := findPair(pairs, "case1")
	if case1Pair == nil {
		t.Fatal("case1 pair not found")
	}
	if !case1Pair.Valid {
		t.Error("case1 pair should be valid")
	}
	expectedDiff := 0.2 // 0.9 - 0.7
	if math.Abs(case1Pair.Difference-expectedDiff) > 0.001 {
		t.Errorf("case1 difference = %.2f, want %.2f", case1Pair.Difference, expectedDiff)
	}

	// Check case2 - valid pair
	case2Pair := findPair(pairs, "case2")
	if case2Pair == nil {
		t.Fatal("case2 pair not found")
	}
	if !case2Pair.Valid {
		t.Error("case2 pair should be valid")
	}

	// Check case3 - invalid pair (repetition mismatch)
	case3Pair := findPair(pairs, "case3")
	if case3Pair == nil {
		t.Fatal("case3 pair not found")
	}
	if case3Pair.Valid {
		t.Error("case3 pair should be invalid")
	}
	if case3Pair.InvalidReason != "repetition_mismatch" {
		t.Errorf("case3 invalid reason = %s, want repetition_mismatch", case3Pair.InvalidReason)
	}

	// Check case4 - invalid pair (no baseline)
	case4Pair := findPair(pairs, "case4")
	if case4Pair == nil {
		t.Fatal("case4 pair not found")
	}
	if case4Pair.Valid {
		t.Error("case4 pair should be invalid")
	}
	if case4Pair.InvalidReason != "case_id_mismatch" {
		t.Errorf("case4 invalid reason = %s, want case_id_mismatch", case4Pair.InvalidReason)
	}
}

func TestValidatePairing(t *testing.T) {
	baselineTrial := TrialResult{
		CaseID:          "case1",
		ModelHash:       "sha256:model",
		EnvironmentHash: "sha256:env",
		GraderHash:      "sha256:grader",
		RepetitionIndex: 0,
	}

	tests := []struct {
		name           string
		candidateTrial TrialResult
		wantValid      bool
		wantReason     string
	}{
		{
			name: "valid pairing",
			candidateTrial: TrialResult{
				CaseID:          "case1",
				ModelHash:       "sha256:model",
				EnvironmentHash: "sha256:env",
				GraderHash:      "sha256:grader",
				RepetitionIndex: 0,
			},
			wantValid:  true,
			wantReason: "",
		},
		{
			name: "case_id mismatch",
			candidateTrial: TrialResult{
				CaseID:          "case2",
				ModelHash:       "sha256:model",
				EnvironmentHash: "sha256:env",
				GraderHash:      "sha256:grader",
				RepetitionIndex: 0,
			},
			wantValid:  false,
			wantReason: "case_id_mismatch",
		},
		{
			name: "model_hash mismatch",
			candidateTrial: TrialResult{
				CaseID:          "case1",
				ModelHash:       "sha256:different_model",
				EnvironmentHash: "sha256:env",
				GraderHash:      "sha256:grader",
				RepetitionIndex: 0,
			},
			wantValid:  false,
			wantReason: "model_mismatch",
		},
		{
			name: "environment_hash mismatch",
			candidateTrial: TrialResult{
				CaseID:          "case1",
				ModelHash:       "sha256:model",
				EnvironmentHash: "sha256:different_env",
				GraderHash:      "sha256:grader",
				RepetitionIndex: 0,
			},
			wantValid:  false,
			wantReason: "environment_mismatch",
		},
		{
			name: "grader_hash mismatch",
			candidateTrial: TrialResult{
				CaseID:          "case1",
				ModelHash:       "sha256:model",
				EnvironmentHash: "sha256:env",
				GraderHash:      "sha256:different_grader",
				RepetitionIndex: 0,
			},
			wantValid:  false,
			wantReason: "grader_mismatch",
		},
		{
			name: "repetition_index mismatch",
			candidateTrial: TrialResult{
				CaseID:          "case1",
				ModelHash:       "sha256:model",
				EnvironmentHash: "sha256:env",
				GraderHash:      "sha256:grader",
				RepetitionIndex: 1,
			},
			wantValid:  false,
			wantReason: "repetition_mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, reason := ValidatePairing(baselineTrial, tt.candidateTrial)

			if valid != tt.wantValid {
				t.Errorf("valid = %v, want %v", valid, tt.wantValid)
			}

			if reason != tt.wantReason {
				t.Errorf("reason = %s, want %s", reason, tt.wantReason)
			}
		})
	}
}

func findPair(pairs []Pair, caseID string) *Pair {
	for i := range pairs {
		if pairs[i].CaseID == caseID {
			return &pairs[i]
		}
	}
	return nil
}
