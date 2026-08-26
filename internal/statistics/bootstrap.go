package statistics

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"

	"github.com/Lin-xun1113/SkillGate/internal/metrics"
)

// BootstrapConfig configures bootstrap CI calculation
type BootstrapConfig struct {
	NumResamples int     `json:"num_resamples"` // Default 2000
	CILevel      float64 `json:"ci_level"`      // Default 0.95
	Seed         int64   `json:"seed"`          // Fixed seed for reproducibility
	Parallel     bool    `json:"parallel"`      // Parallel execution
}

// BootstrapResult represents bootstrap CI results
type BootstrapResult struct {
	MeanEstimate float64 `json:"mean_estimate"`
	CILower      float64 `json:"ci_lower"`
	CIUpper      float64 `json:"ci_upper"`
	NCases       int     `json:"n_cases"`
	Method       string  `json:"method"`
}

// DefaultBootstrapConfig returns default bootstrap configuration
func DefaultBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		NumResamples: 2000,
		CILevel:      0.95,
		Seed:         42,
		Parallel:     true,
	}
}

// ClusterBootstrapCI calculates bootstrap confidence interval
// using cluster bootstrap (resampling at case level)
func ClusterBootstrapCI(pairs []metrics.Pair, config BootstrapConfig) (*BootstrapResult, error) {
	// Extract valid pairs only
	var validDiffs []float64
	for _, pair := range pairs {
		if pair.Valid {
			validDiffs = append(validDiffs, pair.Difference)
		}
	}

	if len(validDiffs) < 2 {
		return &BootstrapResult{
			MeanEstimate: 0,
			CILower:      0,
			CIUpper:      0,
			NCases:       len(validDiffs),
			Method:       "insufficient_data",
		}, fmt.Errorf("insufficient data: need at least 2 valid pairs, got %d", len(validDiffs))
	}

	// Calculate point estimate
	meanEstimate := calculateMean(validDiffs)

	// Perform bootstrap resampling
	var bootstrapMeans []float64
	if config.Parallel {
		bootstrapMeans = parallelResample(validDiffs, config.NumResamples, config.Seed)
	} else {
		bootstrapMeans = sequentialResample(validDiffs, config.NumResamples, config.Seed)
	}

	// Calculate percentile CI
	sort.Float64s(bootstrapMeans)
	alpha := 1.0 - config.CILevel
	lowerIdx := int(float64(len(bootstrapMeans)) * alpha / 2.0)
	upperIdx := int(float64(len(bootstrapMeans)) * (1.0 - alpha/2.0))

	if lowerIdx < 0 {
		lowerIdx = 0
	}
	if upperIdx >= len(bootstrapMeans) {
		upperIdx = len(bootstrapMeans) - 1
	}

	return &BootstrapResult{
		MeanEstimate: meanEstimate,
		CILower:      bootstrapMeans[lowerIdx],
		CIUpper:      bootstrapMeans[upperIdx],
		NCases:       len(validDiffs),
		Method:       "cluster_bootstrap_percentile",
	}, nil
}

// parallelResample performs parallel bootstrap resampling
func parallelResample(diffs []float64, numResamples int, seed int64) []float64 {
	numWorkers := runtime.NumCPU()
	perWorker := numResamples / numWorkers

	results := make([]float64, numResamples)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int, workerSeed int64) {
			defer wg.Done()

			// Each worker gets its own RNG with unique seed
			rng := rand.New(rand.NewSource(workerSeed))

			start := workerID * perWorker
			end := start + perWorker
			if workerID == numWorkers-1 {
				// Last worker handles remainder
				end = numResamples
			}

			for j := start; j < end; j++ {
				sample := resampleWithReplacement(diffs, rng)
				results[j] = calculateMean(sample)
			}
		}(i, seed+int64(i))
	}

	wg.Wait()
	return results
}

// sequentialResample performs sequential bootstrap resampling
func sequentialResample(diffs []float64, numResamples int, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	results := make([]float64, numResamples)

	for i := 0; i < numResamples; i++ {
		sample := resampleWithReplacement(diffs, rng)
		results[i] = calculateMean(sample)
	}

	return results
}

// resampleWithReplacement performs bootstrap resampling with replacement
func resampleWithReplacement(data []float64, rng *rand.Rand) []float64 {
	n := len(data)
	sample := make([]float64, n)

	for i := 0; i < n; i++ {
		idx := rng.Intn(n)
		sample[i] = data[idx]
	}

	return sample
}

// calculateMean calculates mean of a slice
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

// PassAtKResult represents pass@k calculation results
type PassAtKResult struct {
	K          int     `json:"k"`
	PassAtK    float64 `json:"pass_at_k"`
	PassPowerK float64 `json:"pass_power_k"`
	N          int     `json:"n"`
	C          int     `json:"c"`
}

// PassAtK calculates pass@k using HumanEval formula
// pass@k = 1 - C(n-c, k) / C(n, k)
func PassAtK(attempts []bool, k int) (*PassAtKResult, error) {
	n := len(attempts)
	c := countTrue(attempts)

	if k > n {
		return nil, fmt.Errorf("k (%d) > n (%d)", k, n)
	}

	if k <= 0 {
		return nil, fmt.Errorf("k must be positive, got %d", k)
	}

	// Calculate pass@k
	numerator := binomial(n-c, k)
	denominator := binomial(n, k)

	passAtK := 1.0
	if denominator > 0 {
		passAtK = 1.0 - float64(numerator)/float64(denominator)
	}

	// Calculate pass^k
	p := float64(c) / float64(n)
	passPowerK := math.Pow(p, float64(k))

	return &PassAtKResult{
		K:          k,
		PassAtK:    passAtK,
		PassPowerK: passPowerK,
		N:          n,
		C:          c,
	}, nil
}

// countTrue counts number of true values
func countTrue(values []bool) int {
	count := 0
	for _, v := range values {
		if v {
			count++
		}
	}
	return count
}

// binomial calculates binomial coefficient C(n, k) = n! / (k! * (n-k)!)
func binomial(n, k int) int64 {
	if k < 0 || k > n {
		return 0
	}
	if k == 0 || k == n {
		return 1
	}
	if k > n-k {
		k = n - k
	}

	result := int64(1)
	for i := 0; i < k; i++ {
		result = result * int64(n-i) / int64(i+1)
	}

	return result
}

// ResourceDelta represents resource usage delta between baseline and candidate
type ResourceDelta struct {
	BaselineMean  float64 `json:"baseline_mean"`
	CandidateMean float64 `json:"candidate_mean"`
	Delta         float64 `json:"delta"`
	DeltaRatio    float64 `json:"delta_ratio"`
}

// CalculateResourceDelta calculates resource usage delta
func CalculateResourceDelta(baselineValues, candidateValues []float64) *ResourceDelta {
	baselineMean := calculateMean(baselineValues)
	candidateMean := calculateMean(candidateValues)
	delta := candidateMean - baselineMean

	deltaRatio := 0.0
	if baselineMean != 0 {
		deltaRatio = delta / baselineMean
	}

	return &ResourceDelta{
		BaselineMean:  baselineMean,
		CandidateMean: candidateMean,
		Delta:         delta,
		DeltaRatio:    deltaRatio,
	}
}
