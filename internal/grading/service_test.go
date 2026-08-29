package grading

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/grader"
	"github.com/Lin-xun1113/SkillGate/internal/report"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// Mock Store for testing
type mockStore struct {
	experiment        *Experiment
	trials            []TrialResult
	updatedGrades     map[string]json.RawMessage
	statusTransitions []statusTransition
	savedReports      []ExperimentReport
}

type statusTransition struct {
	experimentID string
	from         scheduler.ExperimentStatus
	to           scheduler.ExperimentStatus
}

func (m *mockStore) GetExperimentByID(ctx context.Context, experimentID string) (*Experiment, error) {
	return m.experiment, nil
}

func (m *mockStore) GetTrialResults(ctx context.Context, experimentID string) ([]TrialResult, error) {
	return m.trials, nil
}

func (m *mockStore) UpdateTrialGrades(ctx context.Context, resultID string, grades json.RawMessage) error {
	if m.updatedGrades == nil {
		m.updatedGrades = make(map[string]json.RawMessage)
	}
	m.updatedGrades[resultID] = grades
	return nil
}

func (m *mockStore) TransitionExperimentStatus(ctx context.Context, experimentID string, from, to scheduler.ExperimentStatus) error {
	m.statusTransitions = append(m.statusTransitions, statusTransition{experimentID, from, to})
	return nil
}

func (m *mockStore) SaveReport(ctx context.Context, report *ExperimentReport) error {
	m.savedReports = append(m.savedReports, *report)
	return nil
}

func TestServiceCreation(t *testing.T) {
	store := &mockStore{}
	registry := grader.NewRegistry("/tmp/graders")

	service := NewService(store, registry, "/tmp/artifacts", 5*time.Second)

	if service == nil {
		t.Fatal("expected service to be created")
	}

	if service.artifactsRoot != "/tmp/artifacts" {
		t.Errorf("expected artifactsRoot=/tmp/artifacts, got %s", service.artifactsRoot)
	}

	if service.pollInterval != 5*time.Second {
		t.Errorf("expected pollInterval=5s, got %v", service.pollInterval)
	}
}

func TestProcessExperimentBasicFlow(t *testing.T) {
	// Setup mock store
	store := &mockStore{
		experiment: &Experiment{
			ExperimentID: "exp-001",
			GraderHash:   "grader-hash-001",
			ArtifactsDir: "artifacts/exp-001",
		},
		trials: []TrialResult{
			{
				ResultID:        "result-001",
				TrialID:         "trial-001",
				LogicalTrialID:  "logical-001",
				CaseID:          "case-001",
				Arm:             "without_skill",
				RepetitionIndex: 0,
				ModelHash:       "model-001",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-001/trial-001",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
				InputTokens:     100,
				OutputTokens:    50,
				LatencyMS:       1000,
			},
			{
				ResultID:        "result-002",
				TrialID:         "trial-002",
				LogicalTrialID:  "logical-002",
				CaseID:          "case-001",
				Arm:             "with_skill",
				RepetitionIndex: 0,
				ModelHash:       "model-001",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-001/trial-002",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
				InputTokens:     120,
				OutputTokens:    60,
				LatencyMS:       900,
			},
			{
				ResultID:        "result-003",
				TrialID:         "trial-003",
				LogicalTrialID:  "logical-003",
				CaseID:          "case-002",
				Arm:             "without_skill",
				RepetitionIndex: 0,
				ModelHash:       "model-001",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-001/trial-003",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":false,"score":0.0}],"aggregated_score":0.0}`),
				InputTokens:     80,
				OutputTokens:    40,
				LatencyMS:       800,
			},
			{
				ResultID:        "result-004",
				TrialID:         "trial-004",
				LogicalTrialID:  "logical-004",
				CaseID:          "case-002",
				Arm:             "with_skill",
				RepetitionIndex: 0,
				ModelHash:       "model-001",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-001/trial-004",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
				InputTokens:     130,
				OutputTokens:    65,
				LatencyMS:       950,
			},
		},
	}

	registry := grader.NewRegistry("/tmp/graders")

	// Register method doesn't exist, we need to use LoadFromFile
	// For testing, we'll skip the grader registration since trials already have grades
	// The service will use existing grades from the mock trials

	service := NewService(store, registry, "/tmp/artifacts", 5*time.Second)

	// Process experiment
	ctx := context.Background()
	err := service.ProcessExperiment(ctx, "exp-001")

	if err != nil {
		t.Fatalf("ProcessExperiment failed: %v", err)
	}

	// Verify status transition
	if len(store.statusTransitions) != 1 {
		t.Errorf("expected 1 status transition, got %d", len(store.statusTransitions))
	} else {
		st := store.statusTransitions[0]
		if st.from != scheduler.ExperimentGrading {
			t.Errorf("expected transition from GRADING, got %s", st.from)
		}
		if st.to != scheduler.ExperimentStatus("COMPLETED") {
			t.Errorf("expected transition to COMPLETED, got %s", st.to)
		}
	}

	// Verify report was saved
	if len(store.savedReports) != 1 {
		t.Fatalf("expected 1 saved report, got %d", len(store.savedReports))
	}

	// Regression guard: pairing must actually match without_skill/with_skill
	// arms (this is exactly what the previous "baseline"/"candidate" bug broke —
	// it silently produced 0 valid pairs while every check still passed).
	saved := store.savedReports[0]
	if saved.ValidPairs != 2 {
		t.Errorf("expected 2 valid pairs (case-001, case-002), got %d", saved.ValidPairs)
	}
	if saved.InvalidPairs != 0 {
		t.Errorf("expected 0 invalid pairs, got %d", saved.InvalidPairs)
	}
	if saved.MeanLift == nil {
		t.Fatal("expected mean_lift to be set when there are >=2 valid pairs")
	}
	// case-001 diff = 1.0-1.0 = 0, case-002 diff = 1.0-0.0 = 1.0 -> mean 0.5
	if *saved.MeanLift < 0.49 || *saved.MeanLift > 0.51 {
		t.Errorf("expected mean_lift ~0.5, got %f", *saved.MeanLift)
	}

	reportPath := saved.FilePath
	if reportPath == "" {
		t.Fatal("expected report FilePath to be set")
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected report.json at %s, got error: %v", reportPath, err)
	}
	var written report.Report
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("report.json is not valid JSON: %v", err)
	}
	if written.Metadata.ValidPairs != 2 {
		t.Errorf("report.json valid_pairs = %d, want 2", written.Metadata.ValidPairs)
	}
	if len(written.PassAtK) == 0 {
		t.Error("expected report.json pass_at_k to be populated (candidate arm has pass/fail data)")
	}
	if written.ResourceUsage.TokenDelta.BaselineMean == 0 {
		t.Error("expected report.json resource_usage.token_delta to be populated from trial usage")
	}
	if err := report.ValidateSchema(data); err != nil {
		t.Errorf("generated report.json failed schema validation: %v", err)
	}

	// Regression guard: experiment_reports.file_hash is NOT NULL and
	// report_type is constrained to ('json','markdown','html') in
	// migrations/00003_grading_support.sql. A mock store can't catch a
	// violation of those real-Postgres constraints, so assert the values
	// we hand to SaveReport would actually satisfy them.
	if saved.FileHash == "" {
		t.Error("expected FileHash to be set (experiment_reports.file_hash is NOT NULL)")
	}
	switch saved.ReportType {
	case "json", "markdown", "html":
	default:
		t.Errorf("ReportType = %q, want one of json/markdown/html (experiment_reports.report_type CHECK constraint)", saved.ReportType)
	}
}

func TestHasActualGrades(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{
			name:     "nil grades",
			input:    nil,
			expected: false,
		},
		{
			name:     "empty byte slice",
			input:    []byte{},
			expected: false,
		},
		{
			name:     "empty JSON object",
			input:    []byte(`{}`),
			expected: false,
		},
		{
			name:     "empty graders array",
			input:    []byte(`{"graders":[],"aggregated_score":0}`),
			expected: false,
		},
		{
			name:     "actual grader results",
			input:    []byte(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
			expected: true,
		},
		{
			name:     "invalid JSON",
			input:    []byte(`{invalid}`),
			expected: false,
		},
		{
			name:     "only aggregated_score present",
			input:    []byte(`{"aggregated_score":0.5}`),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasActualGrades(tt.input)
			if result != tt.expected {
				t.Errorf("hasActualGrades(%s) = %v, want %v", string(tt.input), result, tt.expected)
			}
		})
	}
}

func TestEmptyGradesNotSkippedInProcessing(t *testing.T) {
	// Verify that empty {} grades are treated as "not graded yet"
	emptyGrades := []byte(`{}`)

	// This should return false so the trial is not skipped
	if hasActualGrades(emptyGrades) {
		t.Error("Empty {} should be treated as not graded yet")
	}

	// Verify that after json.Marshal, {} still doesn't have graders
	var manifest map[string]interface{}
	json.Unmarshal(emptyGrades, &manifest)
	marshaled, _ := json.Marshal(manifest)

	if hasActualGrades(marshaled) {
		t.Error("Marshaled empty object should still be treated as not graded")
	}
}

func TestProcessExperiment_PairingValidationEnforced(t *testing.T) {
	// Setup mock store with trials that have model hash mismatches
	store := &mockStore{
		experiment: &Experiment{
			ExperimentID: "exp-002",
			GraderHash:   "grader-hash-001",
			ArtifactsDir: "artifacts/exp-002",
		},
		trials: []TrialResult{
			{
				ResultID:        "result-001",
				TrialID:         "trial-001",
				LogicalTrialID:  "logical-001",
				CaseID:          "case-001",
				Arm:             "without_skill",
				RepetitionIndex: 0,
				ModelHash:       "model-001",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-002/trial-001",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":false,"score":0.0}],"aggregated_score":0.0}`),
				InputTokens:     100,
				OutputTokens:    50,
				LatencyMS:       1000,
			},
			{
				ResultID:        "result-002",
				TrialID:         "trial-002",
				LogicalTrialID:  "logical-002",
				CaseID:          "case-001",
				Arm:             "with_skill",
				RepetitionIndex: 0,
				ModelHash:       "model-002", // Different model hash - should invalidate pair
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-002/trial-002",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
				InputTokens:     120,
				OutputTokens:    60,
				LatencyMS:       900,
			},
		},
	}

	registry := grader.NewRegistry("/tmp/graders")
	service := NewService(store, registry, "/tmp/artifacts", 5*time.Second)

	// Process experiment
	ctx := context.Background()
	err := service.ProcessExperiment(ctx, "exp-002")

	if err != nil {
		t.Fatalf("ProcessExperiment failed: %v", err)
	}

	// Verify report was saved
	if len(store.savedReports) != 1 {
		t.Fatalf("expected 1 saved report, got %d", len(store.savedReports))
	}

	report := store.savedReports[0]

	// Should have 0 valid pairs and 1 invalid pair due to model hash mismatch
	if report.ValidPairs != 0 {
		t.Errorf("expected 0 valid pairs, got %d", report.ValidPairs)
	}

	if report.InvalidPairs != 1 {
		t.Errorf("expected 1 invalid pair (model_mismatch), got %d", report.InvalidPairs)
	}

	// Lift should be 0 since there are no valid pairs
	if report.MeanLift != nil && *report.MeanLift != 0 {
		t.Errorf("expected mean_lift to be 0 or nil with no valid pairs, got %f", *report.MeanLift)
	}
}

func TestProcessExperimentMissingGradesDoesNotComplete(t *testing.T) {
	store := &mockStore{
		experiment: &Experiment{ExperimentID: "exp-incomplete", GraderHash: "grader-missing", TotalTrials: 1},
		trials: []TrialResult{{
			ResultID: "result-1", TrialID: "trial-1", LogicalTrialID: "logical-1", CaseID: "case-1", Arm: "with_skill",
			RepetitionIndex: 0, ModelHash: "model", EnvironmentHash: "env", GraderHash: "grader-missing",
			Grades: json.RawMessage(`{}`),
		}},
	}
	service := NewService(store, grader.NewRegistry(t.TempDir()), t.TempDir(), time.Second)
	err := service.ProcessExperiment(context.Background(), "exp-incomplete")
	if !errors.Is(err, ErrIncompleteEvidence) {
		t.Fatalf("expected ErrIncompleteEvidence, got %v", err)
	}
	if len(store.statusTransitions) != 0 {
		t.Fatalf("incomplete grading must not transition to COMPLETED: %+v", store.statusTransitions)
	}
	if len(store.savedReports) != 1 {
		t.Fatalf("expected incomplete report to be persisted, got %d", len(store.savedReports))
	}
	reportData, readErr := os.ReadFile(store.savedReports[0].FilePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var persisted report.Report
	if err := json.Unmarshal(reportData, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Evidence.Complete {
		t.Fatal("incomplete report must not claim complete evidence")
	}
}
