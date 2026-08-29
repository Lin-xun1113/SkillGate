package metrics

import (
	"fmt"
	"math"
)

// CaseScore represents aggregated scores for a single case-arm combination
type CaseScore struct {
	CaseID      string    `json:"case_id"`
	Arm         string    `json:"arm"`
	Repetitions int       `json:"repetitions"`
	MeanScore   float64   `json:"mean_score"`
	StdDev      float64   `json:"std_dev"`
	TrialScores []float64 `json:"trial_scores"`
	TrialPassed []bool    `json:"trial_passed"`
}

// Pair represents a matched baseline-candidate pair
type Pair struct {
	CaseID               string  `json:"case_id"`
	BaselineScore        float64 `json:"baseline_score"`
	CandidateScore       float64 `json:"candidate_score"`
	Difference           float64 `json:"difference"`
	BaselineRepetitions  int     `json:"baseline_repetitions"`
	CandidateRepetitions int     `json:"candidate_repetitions"`
	Valid                bool    `json:"valid"`
	InvalidReason        string  `json:"invalid_reason,omitempty"`
}

// TrialResult represents a single trial's grading result
type TrialResult struct {
	TrialID         string  `json:"trial_id"`
	LogicalTrialID  string  `json:"logical_trial_id"`
	ExperimentID    string  `json:"experiment_id"`
	CaseID          string  `json:"case_id"`
	Arm             string  `json:"arm"`
	RepetitionIndex int     `json:"repetition_index"`
	AggregatedScore float64 `json:"aggregated_score"`
	Passed          bool    `json:"passed"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	LatencyMS       int     `json:"latency_ms"`
	ToolCalls       int     `json:"tool_calls,omitempty"`
	CostUSD         float64 `json:"cost_usd,omitempty"`
	ModelHash       string  `json:"model_hash"`
	EnvironmentHash string  `json:"environment_hash"`
	GraderHash      string  `json:"grader_hash"`
	EvaluationMode  string  `json:"evaluation_mode,omitempty"`
	Population      string  `json:"population,omitempty"`
	Polarity        string  `json:"polarity,omitempty"`
	EvidenceStatus  string  `json:"evidence_status,omitempty"`
}

// AggregateCaseScores aggregates trial scores into case-level scores
func AggregateCaseScores(trials []TrialResult) ([]CaseScore, error) {
	// Group by (case_id, arm)
	groups := make(map[string][]TrialResult)
	for _, trial := range trials {
		key := fmt.Sprintf("%s:%s", trial.CaseID, trial.Arm)
		groups[key] = append(groups[key], trial)
	}

	var caseScores []CaseScore
	for _, groupTrials := range groups {
		if len(groupTrials) == 0 {
			continue
		}

		// Extract case_id and arm
		caseID := groupTrials[0].CaseID
		arm := groupTrials[0].Arm

		// Collect scores
		var scores []float64
		var passed []bool
		for _, trial := range groupTrials {
			scores = append(scores, trial.AggregatedScore)
			passed = append(passed, trial.Passed)
		}

		// Calculate mean and stddev
		mean := calculateMean(scores)
		stddev := calculateStdDev(scores, mean)

		caseScores = append(caseScores, CaseScore{
			CaseID:      caseID,
			Arm:         arm,
			Repetitions: len(scores),
			MeanScore:   mean,
			StdDev:      stddev,
			TrialScores: scores,
			TrialPassed: passed,
		})
	}

	return caseScores, nil
}

// PairCases matches baseline and candidate case scores and validates pairing rules
func PairCases(baseline, candidate []CaseScore, baselineArm, candidateArm string) ([]Pair, error) {
	return pairCasesLegacy(baseline, candidate, baselineArm, candidateArm)
}

// PairCasesWithValidation matches and validates baseline-candidate pairs using trial-level data
func PairCasesWithValidation(baseline, candidate []CaseScore, baselineTrials, candidateTrials []TrialResult, baselineArm, candidateArm string) ([]Pair, error) {
	// Create lookup map for baseline trials by (caseID, repetitionIndex)
	baselineTrialMap := make(map[string]TrialResult)
	for _, trial := range baselineTrials {
		if trial.Arm == baselineArm {
			key := fmt.Sprintf("%s:%d", trial.CaseID, trial.RepetitionIndex)
			baselineTrialMap[key] = trial
		}
	}

	// Create lookup map for baseline case scores
	baselineMap := make(map[string]CaseScore)
	for _, cs := range baseline {
		if cs.Arm == baselineArm {
			baselineMap[cs.CaseID] = cs
		}
	}

	// Track pairing validation results per case
	caseValidation := make(map[string]struct {
		valid         bool
		invalidReason string
	})

	// Validate each candidate trial against its baseline counterpart
	for _, candidateTrial := range candidateTrials {
		if candidateTrial.Arm != candidateArm {
			continue
		}

		key := fmt.Sprintf("%s:%d", candidateTrial.CaseID, candidateTrial.RepetitionIndex)
		baselineTrial, ok := baselineTrialMap[key]
		if !ok {
			// Missing baseline trial for this repetition
			caseValidation[candidateTrial.CaseID] = struct {
				valid         bool
				invalidReason string
			}{false, "missing_baseline_trial"}
			continue
		}

		// Validate pairing using ValidatePairing
		valid, reason := ValidatePairing(baselineTrial, candidateTrial)
		if !valid {
			// Record first validation failure for this case
			if _, exists := caseValidation[candidateTrial.CaseID]; !exists {
				caseValidation[candidateTrial.CaseID] = struct {
					valid         bool
					invalidReason string
				}{false, reason}
			}
		} else {
			// Only mark as valid if no previous failure
			if _, exists := caseValidation[candidateTrial.CaseID]; !exists {
				caseValidation[candidateTrial.CaseID] = struct {
					valid         bool
					invalidReason string
				}{true, ""}
			}
		}
	}

	// Build pairs from case scores, applying validation results
	var pairs []Pair
	for _, candidateCS := range candidate {
		if candidateCS.Arm != candidateArm {
			continue
		}

		baselineCS, ok := baselineMap[candidateCS.CaseID]
		if !ok {
			// Case ID not found in baseline
			pairs = append(pairs, Pair{
				CaseID:               candidateCS.CaseID,
				CandidateRepetitions: candidateCS.Repetitions,
				Valid:                false,
				InvalidReason:        "case_id_mismatch",
			})
			continue
		}

		// Check validation result
		validation, hasValidation := caseValidation[candidateCS.CaseID]
		valid := hasValidation && validation.valid
		invalidReason := validation.invalidReason

		// Additional check: repetition count mismatch
		if baselineCS.Repetitions != candidateCS.Repetitions {
			valid = false
			if invalidReason == "" {
				invalidReason = "repetition_count_mismatch"
			}
		}

		// Calculate difference
		diff := candidateCS.MeanScore - baselineCS.MeanScore

		pairs = append(pairs, Pair{
			CaseID:               candidateCS.CaseID,
			BaselineScore:        baselineCS.MeanScore,
			CandidateScore:       candidateCS.MeanScore,
			Difference:           diff,
			BaselineRepetitions:  baselineCS.Repetitions,
			CandidateRepetitions: candidateCS.Repetitions,
			Valid:                valid,
			InvalidReason:        invalidReason,
		})
	}

	return pairs, nil
}

// pairCasesLegacy is the original implementation without trial-level validation
func pairCasesLegacy(baseline, candidate []CaseScore, baselineArm, candidateArm string) ([]Pair, error) {
	// Create lookup map for baseline
	baselineMap := make(map[string]CaseScore)
	for _, cs := range baseline {
		if cs.Arm == baselineArm {
			baselineMap[cs.CaseID] = cs
		}
	}

	// Match with candidate
	var pairs []Pair
	for _, candidateCS := range candidate {
		if candidateCS.Arm != candidateArm {
			continue
		}

		baselineCS, ok := baselineMap[candidateCS.CaseID]
		if !ok {
			// Case ID not found in baseline
			pairs = append(pairs, Pair{
				CaseID:               candidateCS.CaseID,
				CandidateRepetitions: candidateCS.Repetitions,
				Valid:                false,
				InvalidReason:        "case_id_mismatch",
			})
			continue
		}

		// Check pairing validity
		valid := true
		invalidReason := ""

		if baselineCS.Repetitions != candidateCS.Repetitions {
			valid = false
			invalidReason = "repetition_mismatch"
		}

		// Calculate difference
		diff := candidateCS.MeanScore - baselineCS.MeanScore

		pairs = append(pairs, Pair{
			CaseID:               candidateCS.CaseID,
			BaselineScore:        baselineCS.MeanScore,
			CandidateScore:       candidateCS.MeanScore,
			Difference:           diff,
			BaselineRepetitions:  baselineCS.Repetitions,
			CandidateRepetitions: candidateCS.Repetitions,
			Valid:                valid,
			InvalidReason:        invalidReason,
		})
	}

	return pairs, nil
}

// ValidatePairing validates pairing rules for trials
func ValidatePairing(baselineTrial, candidateTrial TrialResult) (bool, string) {
	if baselineTrial.CaseID != candidateTrial.CaseID {
		return false, "case_id_mismatch"
	}

	if baselineTrial.ModelHash != candidateTrial.ModelHash {
		return false, "model_mismatch"
	}

	if baselineTrial.EnvironmentHash != candidateTrial.EnvironmentHash {
		return false, "environment_mismatch"
	}

	if baselineTrial.GraderHash != candidateTrial.GraderHash {
		return false, "grader_mismatch"
	}

	if baselineTrial.RepetitionIndex != candidateTrial.RepetitionIndex {
		return false, "repetition_mismatch"
	}

	return true, ""
}

// Helper functions

func calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateStdDev(values []float64, mean float64) float64 {
	if len(values) <= 1 {
		return 0
	}

	sumSquares := 0.0
	for _, v := range values {
		diff := v - mean
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(len(values)-1)
	return math.Sqrt(variance)
}
