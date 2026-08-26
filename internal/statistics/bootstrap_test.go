package statistics

import (
	"math"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/metrics"
)

func TestClusterBootstrapCI(t *testing.T) {
	// Test case with known differences
	pairs := []metrics.Pair{
		{CaseID: "case1", BaselineScore: 0.5, CandidateScore: 0.7, Difference: 0.2, Valid: true},
		{CaseID: "case2", BaselineScore: 0.6, CandidateScore: 0.8, Difference: 0.2, Valid: true},
		{CaseID: "case3", BaselineScore: 0.4, CandidateScore: 0.6, Difference: 0.2, Valid: true},
		{CaseID: "case4", BaselineScore: 0.7, CandidateScore: 0.85, Difference: 0.15, Valid: true},
		{CaseID: "case5", BaselineScore: 0.5, CandidateScore: 0.75, Difference: 0.25, Valid: true},
	}

	config := BootstrapConfig{
		NumResamples: 1000,
		CILevel:      0.95,
		Seed:         42,
		Parallel:     false,
	}

	result, err := ClusterBootstrapCI(pairs, config)
	if err != nil {
		t.Fatalf("ClusterBootstrapCI failed: %v", err)
	}

	// Expected mean is 0.2
	expectedMean := 0.2
	if math.Abs(result.MeanEstimate-expectedMean) > 0.01 {
		t.Errorf("expected mean ~%.2f, got %.4f", expectedMean, result.MeanEstimate)
	}

	// CI should not cross zero (all differences are positive)
	if result.CILower < 0 {
		t.Errorf("CI lower bound should be positive, got %.4f", result.CILower)
	}

	if result.CIUpper <= result.CILower {
		t.Errorf("CI upper (%.4f) should be > lower (%.4f)", result.CIUpper, result.CILower)
	}

	if result.NCases != 5 {
		t.Errorf("expected 5 cases, got %d", result.NCases)
	}
}

func TestClusterBootstrapCI_InsufficientData(t *testing.T) {
	pairs := []metrics.Pair{
		{CaseID: "case1", BaselineScore: 0.5, CandidateScore: 0.7, Difference: 0.2, Valid: true},
	}

	config := DefaultBootstrapConfig()

	_, err := ClusterBootstrapCI(pairs, config)
	if err == nil {
		t.Error("expected error for insufficient data")
	}
}

func TestClusterBootstrapCI_InvalidPairs(t *testing.T) {
	// All pairs invalid
	pairs := []metrics.Pair{
		{CaseID: "case1", Valid: false, InvalidReason: "case_id_mismatch"},
		{CaseID: "case2", Valid: false, InvalidReason: "repetition_mismatch"},
	}

	config := DefaultBootstrapConfig()

	_, err := ClusterBootstrapCI(pairs, config)
	if err == nil {
		t.Error("expected error for all invalid pairs")
	}
}

func TestParallelResample_Reproducibility(t *testing.T) {
	diffs := []float64{0.1, 0.2, 0.15, 0.25, 0.18}

	// Run twice with same seed
	result1 := parallelResample(diffs, 100, 42)
	result2 := parallelResample(diffs, 100, 42)

	if len(result1) != len(result2) {
		t.Fatalf("results have different lengths: %d vs %d", len(result1), len(result2))
	}

	// Results should be identical with same seed
	for i := range result1 {
		if result1[i] != result2[i] {
			t.Errorf("results differ at index %d: %.6f vs %.6f", i, result1[i], result2[i])
		}
	}
}

func TestPassAtK(t *testing.T) {
	tests := []struct {
		name          string
		attempts      []bool
		k             int
		wantPassAtK   float64
		wantPassPowerK float64
		wantError     bool
	}{
		{
			name:          "3 successes out of 5, k=1",
			attempts:      []bool{true, false, true, false, true},
			k:             1,
			wantPassAtK:   0.6, // 3/5
			wantPassPowerK: 0.6,
			wantError:     false,
		},
		{
			name:          "3 successes out of 5, k=3",
			attempts:      []bool{true, false, true, false, true},
			k:             3,
			wantPassAtK:   1.0, // 1 - C(2,3)/C(5,3) = 1 - 0/10 = 1.0
			wantPassPowerK: 0.216, // (3/5)^3 = 0.216
			wantError:     false,
		},
		{
			name:          "all success",
			attempts:      []bool{true, true, true},
			k:             1,
			wantPassAtK:   1.0,
			wantPassPowerK: 1.0,
			wantError:     false,
		},
		{
			name:          "all failure",
			attempts:      []bool{false, false, false},
			k:             1,
			wantPassAtK:   0.0,
			wantPassPowerK: 0.0,
			wantError:     false,
		},
		{
			name:      "k > n",
			attempts:  []bool{true, false},
			k:         3,
			wantError: true,
		},
		{
			name:      "k <= 0",
			attempts:  []bool{true, false},
			k:         0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := PassAtK(tt.attempts, tt.k)

			if (err != nil) != tt.wantError {
				t.Errorf("PassAtK() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError {
				return
			}

			if math.Abs(result.PassAtK-tt.wantPassAtK) > 0.001 {
				t.Errorf("PassAtK = %.4f, want %.4f", result.PassAtK, tt.wantPassAtK)
			}

			if math.Abs(result.PassPowerK-tt.wantPassPowerK) > 0.001 {
				t.Errorf("PassPowerK = %.4f, want %.4f", result.PassPowerK, tt.wantPassPowerK)
			}
		})
	}
}

func TestBinomial(t *testing.T) {
	tests := []struct {
		n    int
		k    int
		want int64
	}{
		{5, 0, 1},
		{5, 1, 5},
		{5, 2, 10},
		{5, 3, 10},
		{5, 4, 5},
		{5, 5, 1},
		{10, 3, 120},
		{0, 0, 1},
		{5, 6, 0}, // k > n
		{5, -1, 0}, // k < 0
	}

	for _, tt := range tests {
		got := binomial(tt.n, tt.k)
		if got != tt.want {
			t.Errorf("binomial(%d, %d) = %d, want %d", tt.n, tt.k, got, tt.want)
		}
	}
}

func TestCalculateResourceDelta(t *testing.T) {
	baselineValues := []float64{1000, 1100, 1050, 1075, 1025}
	candidateValues := []float64{1200, 1250, 1225, 1275, 1230}

	delta := CalculateResourceDelta(baselineValues, candidateValues)

	expectedBaselineMean := 1050.0
	expectedCandidateMean := 1236.0
	expectedDelta := 186.0
	expectedRatio := 0.177 // approximately

	if math.Abs(delta.BaselineMean-expectedBaselineMean) > 1.0 {
		t.Errorf("BaselineMean = %.2f, want ~%.2f", delta.BaselineMean, expectedBaselineMean)
	}

	if math.Abs(delta.CandidateMean-expectedCandidateMean) > 1.0 {
		t.Errorf("CandidateMean = %.2f, want ~%.2f", delta.CandidateMean, expectedCandidateMean)
	}

	if math.Abs(delta.Delta-expectedDelta) > 1.0 {
		t.Errorf("Delta = %.2f, want ~%.2f", delta.Delta, expectedDelta)
	}

	if math.Abs(delta.DeltaRatio-expectedRatio) > 0.01 {
		t.Errorf("DeltaRatio = %.3f, want ~%.3f", delta.DeltaRatio, expectedRatio)
	}
}
