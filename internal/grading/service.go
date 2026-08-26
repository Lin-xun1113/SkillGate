package grading

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/grader"
	"github.com/Lin-xun1113/SkillGate/internal/metrics"
	"github.com/Lin-xun1113/SkillGate/internal/releasegate"
	"github.com/Lin-xun1113/SkillGate/internal/report"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/Lin-xun1113/SkillGate/internal/statistics"
	"github.com/Lin-xun1113/SkillGate/internal/strategy"
)

// Store defines the database operations needed by the Grading Service
type Store interface {
	// GetExperimentByID retrieves an experiment
	GetExperimentByID(ctx context.Context, experimentID string) (*Experiment, error)

	// GetTrialResults retrieves all trial results for an experiment
	GetTrialResults(ctx context.Context, experimentID string) ([]TrialResult, error)

	// UpdateTrialGrades updates the grades field for a trial result
	UpdateTrialGrades(ctx context.Context, resultID string, grades json.RawMessage) error

	// TransitionExperimentStatus updates experiment status
	TransitionExperimentStatus(ctx context.Context, experimentID string, from, to scheduler.ExperimentStatus) error

	// SaveReport saves a report to the database
	SaveReport(ctx context.Context, report *ExperimentReport) error
}

// Experiment represents the experiment metadata
type Experiment struct {
	ExperimentID string
	GraderHash   string
	PolicyHash   string
	ArtifactsDir string
	TotalTrials  int
}

// TrialResult represents a trial result with metadata
type TrialResult struct {
	ResultID        string
	TrialID         string
	LogicalTrialID  string
	CaseID          string
	Arm             string
	RepetitionIndex int
	ModelHash       string
	EnvironmentHash string
	GraderHash      string
	EvaluationMode  string
	Population      string
	Polarity        string
	ArtifactsDir    string
	Grades          json.RawMessage
	InputTokens     int
	OutputTokens    int
	LatencyMS       int
}

// ExperimentReport represents a report record. ReportType must be one of
// "json", "markdown", "html" per the experiment_reports.report_type CHECK
// constraint (migrations/00003_grading_support.sql); FileHash must be a
// non-empty content hash of FilePath's contents per the file_hash NOT NULL
// constraint on the same table.
type ExperimentReport struct {
	ReportID                 string
	ExperimentID             string
	ReportType               string
	FilePath                 string
	FileHash                 string
	ValidPairs               int
	InvalidPairs             int
	MeanLift                 *float64
	CILower                  *float64
	CIUpper                  *float64
	StatisticallySignificant *bool
	CreatedAt                time.Time
}

// Service orchestrates the grading pipeline
type Service struct {
	store          Store
	graderRegistry *grader.Registry
	artifactsRoot  string
	pollInterval   time.Duration
	shutdownCh     chan struct{}
}

// NewService creates a new Grading Service
func NewService(store Store, graderRegistry *grader.Registry, artifactsRoot string, pollInterval time.Duration) *Service {
	return &Service{
		store:          store,
		graderRegistry: graderRegistry,
		artifactsRoot:  artifactsRoot,
		pollInterval:   pollInterval,
		shutdownCh:     make(chan struct{}),
	}
}

// Start begins polling for experiments in GRADING status
func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	log.Println("Grading Service started")

	for {
		select {
		case <-ctx.Done():
			log.Println("Grading Service stopped")
			return
		case <-s.shutdownCh:
			log.Println("Grading Service shutdown requested")
			return
		case <-ticker.C:
			// Poll for experiments in GRADING status
			// This would need a query method in Store
			// For now, we'll provide the ProcessExperiment method for external triggering
		}
	}
}

// Stop stops the grading service
func (s *Service) Stop() {
	close(s.shutdownCh)
}

// ProcessExperiment executes the full grading pipeline for an experiment
func (s *Service) ProcessExperiment(ctx context.Context, experimentID string) error {
	log.Printf("Processing experiment %s for grading", experimentID)

	// 1. Get experiment metadata
	experiment, err := s.store.GetExperimentByID(ctx, experimentID)
	if err != nil {
		return fmt.Errorf("failed to get experiment: %w", err)
	}

	// 2. Create executor
	executor := grader.NewExecutor(s.graderRegistry, s.artifactsRoot)

	// 3. Get all trial results
	trials, err := s.store.GetTrialResults(ctx, experimentID)
	if err != nil {
		return fmt.Errorf("failed to get trial results: %w", err)
	}

	// 4. Execute grader for each trial
	var trialResults []metrics.TrialResult
	for _, trial := range trials {
		// Skip if already graded
		if len(trial.Grades) > 0 {
			// Parse existing grades
			var gradesManifest grader.GradesManifest
			if err := json.Unmarshal(trial.Grades, &gradesManifest); err == nil {
				trialResults = append(trialResults, metrics.TrialResult{
					TrialID:         trial.TrialID,
					LogicalTrialID:  trial.LogicalTrialID,
					ExperimentID:    experimentID,
					CaseID:          trial.CaseID,
					Arm:             trial.Arm,
					RepetitionIndex: trial.RepetitionIndex,
					ModelHash:       trial.ModelHash,
					EnvironmentHash: trial.EnvironmentHash,
					GraderHash:      trial.GraderHash,
					AggregatedScore: gradesManifest.AggregatedScore,
					Passed:          allGradersPassed(gradesManifest),
					InputTokens:     trial.InputTokens,
					OutputTokens:    trial.OutputTokens,
					LatencyMS:       trial.LatencyMS,
				})
			}
			continue
		}

		// Execute grader (returns a single GradeResult)
		result, err := executor.Execute(ctx, experiment.GraderHash, experimentID, trial.TrialID)
		if err != nil {
			log.Printf("Grading failed for trial %s: %v", trial.TrialID, err)
			continue
		}

		// For now, we have one grader result per trial
		// Build grades manifest with a single grader
		gradesManifest := grader.GradesManifest{
			Graders:         []grader.GradeResult{*result},
			AggregatedScore: result.Score,
		}

		gradesJSON, err := json.Marshal(gradesManifest)
		if err != nil {
			return fmt.Errorf("failed to marshal grades: %w", err)
		}

		// Save grades to database
		if err := s.store.UpdateTrialGrades(ctx, trial.ResultID, gradesJSON); err != nil {
			return fmt.Errorf("failed to save grades: %w", err)
		}

		trialResults = append(trialResults, metrics.TrialResult{
			TrialID:         trial.TrialID,
			LogicalTrialID:  trial.LogicalTrialID,
			ExperimentID:    experimentID,
			CaseID:          trial.CaseID,
			Arm:             trial.Arm,
			RepetitionIndex: trial.RepetitionIndex,
			ModelHash:       trial.ModelHash,
			EnvironmentHash: trial.EnvironmentHash,
			GraderHash:      trial.GraderHash,
			AggregatedScore: result.Score,
			Passed:          result.Passed,
			InputTokens:     trial.InputTokens,
			OutputTokens:    trial.OutputTokens,
			LatencyMS:       trial.LatencyMS,
		})
	}

	// 5. Aggregate metrics
	caseScores, err := metrics.AggregateCaseScores(trialResults)
	if err != nil {
		return fmt.Errorf("failed to aggregate case scores: %w", err)
	}

	// 6. Pair baseline and candidate
	var baselineScores, candidateScores []metrics.CaseScore
	for _, cs := range caseScores {
		if cs.Arm == "without_skill" {
			baselineScores = append(baselineScores, cs)
		} else if cs.Arm == "with_skill" {
			candidateScores = append(candidateScores, cs)
		}
	}

	pairs, err := metrics.PairCases(baselineScores, candidateScores, "without_skill", "with_skill")
	if err != nil {
		return fmt.Errorf("failed to pair cases: %w", err)
	}

	// 7. Statistical analysis
	bootstrapResult, err := statistics.ClusterBootstrapCI(pairs, statistics.DefaultBootstrapConfig())
	if err != nil {
		log.Printf("Bootstrap CI failed: %v", err)
	}

	passAtK := computePassAtK(candidateScores)
	resourceUsage := computeResourceUsage(trials)

	caseMetaByID := make(map[string]metrics.CaseMeta)
	triggerInputs := make([]metrics.TriggerInput, 0, len(trials))
	securityFindings := make([]metrics.SecurityFinding, 0)
	for _, trial := range trials {
		if trial.EvaluationMode != "" {
			caseMetaByID[trial.CaseID] = metrics.CaseMeta{
				CaseID: trial.CaseID, EvaluationMode: trial.EvaluationMode,
				Population: trial.Population, Polarity: trial.Polarity,
			}
		}
		if trial.EvaluationMode == "autonomous_trigger" {
			triggerInputs = append(triggerInputs, metrics.TriggerInput{
				CaseID: trial.CaseID, Arm: trial.Arm, RepetitionIndex: trial.RepetitionIndex,
				Passed: allGradesPassed(trial.Grades), GradesJSON: trial.Grades,
			})
		}
		if trial.EvaluationMode == "security_probe" && trial.Arm == "with_skill" {
			finding, findingErr := metrics.LoadSecurityFinding(metrics.FindingPath(s.artifactsRoot, experimentID, trial.TrialID))
			if findingErr != nil {
				return fmt.Errorf("failed to load security finding for trial %s: %w", trial.TrialID, findingErr)
			}
			finding.CaseID = trial.CaseID
			if !finding.Present {
				finding = securityFindingFromGrades(trial.CaseID, trial.Grades)
			}
			securityFindings = append(securityFindings, finding)
		}
	}
	caseMeta := make([]metrics.CaseMeta, 0, len(caseMetaByID))
	for _, meta := range caseMetaByID {
		caseMeta = append(caseMeta, meta)
	}
	triggerResult := metrics.AggregateTrigger(caseMeta, triggerInputs)
	securityResult := metrics.AggregateSecurity(caseMeta, securityFindings)

	var snapshotLift, snapshotCILower, snapshotCIUpper float64
	var ciAvailable bool
	validCases := 0
	if bootstrapResult != nil {
		snapshotLift = bootstrapResult.MeanEstimate
		snapshotCILower = bootstrapResult.CILower
		snapshotCIUpper = bootstrapResult.CIUpper
		validCases = bootstrapResult.NCases
		ciAvailable = bootstrapResult.NCases >= 2 && bootstrapResult.Method != "insufficient_data"
	}
	pairingValid := len(pairs) > 0
	for _, pair := range pairs {
		if !pair.Valid {
			pairingValid = false
			break
		}
	}
	var releaseDecision *releasegate.Decision
	if experiment.PolicyHash != "" {
		if _, ok := s.store.(releasegate.Store); !ok {
			return fmt.Errorf("release gate store is required when policy is configured")
		}
	}

	// 8. Generate report
	reportDir := filepath.Join(s.artifactsRoot, experimentID)
	generator := report.NewGenerator(reportDir)

	// Generate report using the Generator API
	reportData, err := generator.Generate(
		experimentID,
		experimentID, // experiment name (use ID as name for now)
		pairs,
		bootstrapResult,
		statistics.DefaultBootstrapConfig(),
		len(trialResults),
		passAtK,
		resourceUsage,
	)
	if err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}
	if releaseDecision != nil {
		reportData.Decision = releasegate.ToReportDecision(*releaseDecision)
	}

	// Save reports
	jsonPath, err := generator.SaveJSON(reportData)
	if err != nil {
		return fmt.Errorf("failed to save JSON report: %w", err)
	}
	log.Printf("JSON report saved to %s", jsonPath)

	mdPath, err := generator.SaveMarkdown(reportData)
	if err != nil {
		return fmt.Errorf("failed to save Markdown report: %w", err)
	}
	log.Printf("Markdown report saved to %s", mdPath)

	htmlPath, err := generator.SaveHTML(reportData)
	if err != nil {
		return fmt.Errorf("failed to save HTML report: %w", err)
	}
	log.Printf("HTML report saved to %s", htmlPath)

	jsonHash, err := hashFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to hash JSON report: %w", err)
	}

	// 9. Save report metadata to database
	var meanLift, reportCILower, reportCIUpper *float64
	var statSig *bool
	if bootstrapResult != nil {
		meanLift = &bootstrapResult.MeanEstimate
		reportCILower = &bootstrapResult.CILower
		reportCIUpper = &bootstrapResult.CIUpper
		sig := bootstrapResult.CILower > 0 || bootstrapResult.CIUpper < 0
		statSig = &sig
	}

	validPairs := 0
	invalidPairs := 0
	for _, p := range pairs {
		if p.Valid {
			validPairs++
		} else {
			invalidPairs++
		}
	}

	reportRecord := &ExperimentReport{
		ReportID:                 fmt.Sprintf("report-%s-%d", experimentID, time.Now().Unix()),
		ExperimentID:             experimentID,
		ReportType:               "json",
		FilePath:                 jsonPath,
		FileHash:                 jsonHash,
		ValidPairs:               validPairs,
		InvalidPairs:             invalidPairs,
		MeanLift:                 meanLift,
		CILower:                  reportCILower,
		CIUpper:                  reportCIUpper,
		StatisticallySignificant: statSig,
		CreatedAt:                time.Now(),
	}

	if err := s.store.SaveReport(ctx, reportRecord); err != nil {
		return fmt.Errorf("failed to save report metadata: %w", err)
	}

	// Evaluate the release gate only after the initial report metadata is saved.
	// The decision is derived from the frozen snapshot, then the report files are
	// rewritten with the decision projection and their final hash is persisted.
	if gateStore, ok := s.store.(releasegate.Store); ok && experiment.PolicyHash != "" {
		// Attempt to retrieve existing immutable snapshot first
		snapshot, snapshotErr := gateStore.GetSnapshot(ctx, experimentID)
		if snapshotErr != nil {
			// No persisted snapshot exists; build and persist a new one
			var newSnap releasegate.Snapshot
			newSnap, snapshotErr = releasegate.BuildSnapshot(releasegate.SnapshotInput{
				ExperimentID: experimentID, PolicyHash: experiment.PolicyHash,
				Lift: snapshotLift, CILower: snapshotCILower, CIUpper: snapshotCIUpper,
				ValidCases: validCases, CIAvailable: ciAvailable, Trigger: triggerResult,
				Security: securityResult, PairingValid: pairingValid, IdentityValid: true,
				IncompleteTrials: incompleteTrialCount(trialResults, experiment.TotalTrials),
				PassAt3: func() float64 {
					if passAtK != nil {
						return passAtK["pass@3"]
					}
					return 0
				}(),
				PassAt3Available: passAtK != nil,
				TokenDeltaRatio: func() float64 {
					if resourceUsage != nil {
						return resourceUsage.TokenDelta.DeltaRatio
					}
					return 0
				}(),
			})
			if snapshotErr != nil {
				return fmt.Errorf("failed to build release snapshot: %w", snapshotErr)
			}
			snapshot = &newSnap
			if err := gateStore.SaveSnapshot(ctx, newSnap); err != nil {
				return fmt.Errorf("failed to save release snapshot: %w", err)
			}
		}
		decision := releasegate.Evaluate(s.loadPolicy(experiment.PolicyHash), *snapshot)
		if err := gateStore.SaveDecision(ctx, decision); err != nil {
			return fmt.Errorf("failed to save release decision: %w", err)
		}
		releaseDecision = &decision
		reportData.Decision = releasegate.ToReportDecision(decision)
		jsonPath, err = generator.SaveJSON(reportData)
		if err != nil {
			return fmt.Errorf("failed to save final JSON report: %w", err)
		}
		mdPath, err = generator.SaveMarkdown(reportData)
		if err != nil {
			return fmt.Errorf("failed to save final Markdown report: %w", err)
		}
		htmlPath, err = generator.SaveHTML(reportData)
		if err != nil {
			return fmt.Errorf("failed to save final HTML report: %w", err)
		}
		jsonHash, err = hashFile(jsonPath)
		if err != nil {
			return fmt.Errorf("failed to hash final JSON report: %w", err)
		}
		reportRecord.FilePath, reportRecord.FileHash = jsonPath, jsonHash
		if err := s.store.SaveReport(ctx, reportRecord); err != nil {
			return fmt.Errorf("failed to save final report metadata: %w", err)
		}
	}

	// 10. Transition experiment to COMPLETED
	if err := s.store.TransitionExperimentStatus(ctx, experimentID, scheduler.ExperimentGrading, scheduler.ExperimentStatus("COMPLETED")); err != nil {
		return fmt.Errorf("failed to transition experiment to COMPLETED: %w", err)
	}

	log.Printf("Experiment %s grading completed", experimentID)
	return nil
}

func allGradesPassed(raw json.RawMessage) bool {
	var manifest grader.GradesManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return false
	}
	return allGradersPassed(manifest)
}

func securityFindingFromGrades(caseID string, raw json.RawMessage) metrics.SecurityFinding {
	var manifest grader.GradesManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return metrics.SecurityFinding{CaseID: caseID}
	}
	for _, result := range manifest.Graders {
		severity, _ := result.Evidence["severity"].(string)
		status, _ := result.Evidence["status"].(string)
		if severity != "" || status != "" {
			return metrics.SecurityFinding{CaseID: caseID, Severity: severity, Status: status, Present: true}
		}
	}
	return metrics.SecurityFinding{CaseID: caseID}
}

func incompleteTrialCount(trialResults []metrics.TrialResult, expected int) int {
	if expected <= 0 {
		return 0
	}
	if missing := expected - len(trialResults); missing > 0 {
		return missing
	}
	return 0
}

func (s *Service) loadPolicy(hash string) *strategy.Policy {
	if hash == "" {
		return nil
	}
	paths := []string{}
	if path := os.Getenv("SKILLGATE_POLICY_PATH"); path != "" {
		paths = append(paths, path)
	}
	if dir := os.Getenv("SKILLGATE_POLICIES_DIR"); dir != "" {
		paths = append(paths, filepath.Join(dir, "conservative-release.yaml"))
	}
	paths = append(paths, filepath.Join("policies", "conservative-release.yaml"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		policy, diags := strategy.ParsePolicyYAML(raw)
		if policy == nil || len(diags) > 0 || policy.Hash != hash {
			continue
		}
		return policy
	}
	return nil
}

// hashFile returns the sha256 hex digest of a file's contents, for populating
// experiment_reports.file_hash (NOT NULL per migrations/00003_grading_support.sql).
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// allGradersPassed reports whether every grader in the manifest passed
// (or, if there are none, false — an empty grades manifest is not a pass).
func allGradersPassed(manifest grader.GradesManifest) bool {
	if len(manifest.Graders) == 0 {
		return false
	}
	for _, g := range manifest.Graders {
		if !g.Passed {
			return false
		}
	}
	return true
}

// computePassAtK pools per-trial pass/fail outcomes across the candidate arm's
// case scores and reports pass@1, pass^1, and (when enough repetitions exist)
// pass@3/pass^3, per D5's HumanEval-style formula. Returns nil when there is no
// candidate data at all, so the report omits the pass_at_k section entirely
// rather than showing a misleading zero.
func computePassAtK(candidateScores []metrics.CaseScore) map[string]float64 {
	var attempts []bool
	for _, cs := range candidateScores {
		attempts = append(attempts, cs.TrialPassed...)
	}
	if len(attempts) == 0 {
		return nil
	}

	result := make(map[string]float64)
	for _, k := range []int{1, 3} {
		if k > len(attempts) {
			continue
		}
		r, err := statistics.PassAtK(attempts, k)
		if err != nil {
			log.Printf("pass@%d calculation failed: %v", k, err)
			continue
		}
		result[fmt.Sprintf("pass@%d", k)] = r.PassAtK
		result[fmt.Sprintf("pass^%d", k)] = r.PassPowerK
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// computeResourceUsage compares token and latency usage between the
// without_skill (baseline) and with_skill (candidate) arms. Returns nil when
// neither arm recorded any usage, so the report omits the resource_usage
// section instead of showing a zero delta that implies "no difference".
func computeResourceUsage(trials []TrialResult) *report.ResourceUsage {
	var baselineTokens, candidateTokens []float64
	var baselineLatency, candidateLatency []float64

	for _, trial := range trials {
		tokens := float64(trial.InputTokens + trial.OutputTokens)
		latency := float64(trial.LatencyMS)
		switch trial.Arm {
		case "without_skill":
			baselineTokens = append(baselineTokens, tokens)
			baselineLatency = append(baselineLatency, latency)
		case "with_skill":
			candidateTokens = append(candidateTokens, tokens)
			candidateLatency = append(candidateLatency, latency)
		}
	}

	if len(baselineTokens) == 0 && len(candidateTokens) == 0 {
		return nil
	}

	return &report.ResourceUsage{
		TokenDelta:   *statistics.CalculateResourceDelta(baselineTokens, candidateTokens),
		LatencyDelta: *statistics.CalculateResourceDelta(baselineLatency, candidateLatency),
	}
}
