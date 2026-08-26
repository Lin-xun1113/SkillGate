package grading

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/grader"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// Mock Store for testing
type mockStore struct {
	experiment   *Experiment
	trials       []TrialResult
	updatedGrades map[string]json.RawMessage
	statusTransitions []statusTransition
	savedReports []ExperimentReport
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
				Arm:             "baseline",
				RepetitionIndex: 0,
				ModelHash:       "model-001",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-001/trial-001",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
			},
			{
				ResultID:        "result-002",
				TrialID:         "trial-002",
				LogicalTrialID:  "logical-002",
				CaseID:          "case-001",
				Arm:             "candidate",
				RepetitionIndex: 0,
				ModelHash:       "model-002",
				EnvironmentHash: "env-001",
				GraderHash:      "grader-hash-001",
				ArtifactsDir:    "artifacts/exp-001/trial-002",
				Grades:          json.RawMessage(`{"graders":[{"grader_id":"g1","passed":true,"score":1.0}],"aggregated_score":1.0}`),
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
		t.Errorf("expected 1 saved report, got %d", len(store.savedReports))
	}
}
