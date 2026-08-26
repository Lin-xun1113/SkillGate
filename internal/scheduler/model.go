package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
)

type ExperimentStatus string
type TrialStatus string
type AttemptStatus string
type Outcome string

const (
	ExperimentCompiled        ExperimentStatus = "COMPILED"
	ExperimentQueued          ExperimentStatus = "QUEUED"
	ExperimentRunning         ExperimentStatus = "RUNNING"
	ExperimentCancelRequested ExperimentStatus = "CANCEL_REQUESTED"
	ExperimentGrading         ExperimentStatus = "GRADING"
	ExperimentCompleted       ExperimentStatus = "COMPLETED"
	ExperimentCancelled       ExperimentStatus = "CANCELLED"
	ExperimentFailed          ExperimentStatus = "FAILED"
)

const (
	TrialPending   TrialStatus = "PENDING"
	TrialLeased    TrialStatus = "LEASED"
	TrialRunning   TrialStatus = "RUNNING"
	TrialRetryWait TrialStatus = "RETRY_WAIT"
	TrialSucceeded TrialStatus = "SUCCEEDED"
	TrialFailed    TrialStatus = "FAILED"
	TrialTimedOut  TrialStatus = "TIMED_OUT"
	TrialCancelled TrialStatus = "CANCELLED"
)

const (
	AttemptPending   AttemptStatus = "PENDING"
	AttemptLeased    AttemptStatus = "LEASED"
	AttemptRunning   AttemptStatus = "RUNNING"
	AttemptSucceeded AttemptStatus = "SUCCEEDED"
	AttemptFailed    AttemptStatus = "FAILED"
	AttemptTimedOut  AttemptStatus = "TIMED_OUT"
	AttemptCancelled AttemptStatus = "CANCELLED"
)

const (
	OutcomeSucceeded Outcome = "SUCCEEDED"
	OutcomeFailed    Outcome = "FAILED"
	OutcomeTimedOut  Outcome = "TIMED_OUT"
	OutcomeCancelled Outcome = "CANCELLED"
)

type ErrorCode string

const (
	CodeDatabaseUnavailable       ErrorCode = "DATABASE_UNAVAILABLE"
	CodeMigrationFailed           ErrorCode = "MIGRATION_FAILED"
	CodeIdentityConflict          ErrorCode = "IDENTITY_CONFLICT"
	CodeTrialNotFound             ErrorCode = "TRIAL_NOT_FOUND"
	CodeNotClaimable              ErrorCode = "NOT_CLAIMABLE"
	CodeOwnerMismatch             ErrorCode = "OWNER_MISMATCH"
	CodeLeaseMismatch             ErrorCode = "LEASE_MISMATCH"
	CodeLeaseExpired              ErrorCode = "LEASE_EXPIRED"
	CodeStatusConflict            ErrorCode = "STATUS_CONFLICT"
	CodeResultConflict            ErrorCode = "RESULT_CONFLICT"
	CodeLogicalTrialTerminal      ErrorCode = "LOGICAL_TRIAL_TERMINAL"
	CodeExperimentCancelRequested ErrorCode = "EXPERIMENT_CANCEL_REQUESTED"
	CodeRetryExhausted            ErrorCode = "RETRY_EXHAUSTED"
	CodeInvalidArgument           ErrorCode = "INVALID_ARGUMENT"
)

type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return string(e.Code) + ": " + e.Message
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Cause }

func CodeOf(err error) ErrorCode {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

type MaterializeOptions struct {
	Priority      int
	MaxAttempts   int
	Timeout       time.Duration
	BudgetTimeout time.Duration
	BackoffBase   time.Duration
	BackoffCap    time.Duration
	Retryable     []retry.Category
}

func (o MaterializeOptions) Policy() retry.Policy {
	categories := make(map[retry.Category]struct{}, len(o.Retryable))
	for _, category := range o.Retryable {
		categories[category] = struct{}{}
	}
	return retry.Policy{MaxAttempts: o.MaxAttempts, Base: o.BackoffBase, Cap: o.BackoffCap, Retryable: categories}
}

type Claim struct {
	ExperimentID    string    `json:"experiment_id"`
	LogicalTrialID  string    `json:"logical_trial_id"`
	TrialID         string    `json:"trial_id"`
	PairID          string    `json:"pair_id,omitempty"`
	Arm             string    `json:"arm,omitempty"`
	Attempt         int       `json:"attempt"`
	WorkerID        string    `json:"worker_id"`
	LeaseToken      string    `json:"lease_token"`
	LeaseGeneration int64     `json:"lease_generation"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at"`
	Deadline        time.Time `json:"deadline"`
	ManifestHash    string    `json:"manifest_hash,omitempty"` // M4: for execution projection
}

type Heartbeat struct {
	TrialID         string
	WorkerID        string
	LeaseToken      string
	LeaseGeneration int64
	EventSequence   int64
	Phase           string
	Usage           ResourceUsage
}

type HeartbeatResult struct {
	LeaseExpiresAt time.Time
}

type Completion struct {
	LogicalTrialID  string
	TrialID         string
	WorkerID        string
	LeaseToken      string
	LeaseGeneration int64
	RequestHash     string
	IdempotencyKey  string
	ManifestHash    string
	Manifest        []byte
	Artifacts       []byte
	Usage           []byte
	Grades          []byte
	Outcome         Outcome
	Category        retry.Category
	EventSequence   int64
}

type MaterializeResult struct {
	ExperimentID  string `json:"experiment_id"`
	LogicalTrials int    `json:"logical_trial_count"`
	Attempts      int    `json:"attempt_count"`
	Idempotent    bool   `json:"idempotent"`
}

type SweepResult struct {
	Processed int `json:"processed"`
	Retried   int `json:"retried"`
	Terminal  int `json:"terminal"`
	Cancelled int `json:"cancelled"`
}

type CancelResult struct {
	ExperimentID string           `json:"experiment_id"`
	Status       ExperimentStatus `json:"status"`
	Idempotent   bool             `json:"idempotent"`
	Cancelled    int              `json:"cancelled_logical_trials"`
}

type CommitResult struct {
	LogicalTrialID string  `json:"logical_trial_id"`
	TrialID        string  `json:"trial_id"`
	ResultID       string  `json:"result_id,omitempty"`
	ManifestHash   string  `json:"result_manifest_hash"`
	Outcome        Outcome `json:"outcome"`
	Idempotent     bool    `json:"idempotent"`
	RetryScheduled bool    `json:"retry_scheduled"`
	NextTrialID    string  `json:"next_trial_id,omitempty"`
}

type ResourceUsage struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ElapsedMS       int64 `json:"elapsed_ms"`
	ToolCalls       int32 `json:"tool_calls"`
	PeakMemoryBytes int64 `json:"peak_memory_bytes"`
}

type Event struct {
	EventID         string
	TrialID         string
	LogicalTrialID  string
	ExperimentID    string
	AttemptNo       int
	WorkerID        string
	LeaseToken      string
	LeaseGeneration int64
	Sequence        int64
	EventType       string
	OccurredAt      time.Time
	Payload         []byte
	PayloadHash     string
}

type EventResult struct {
	Status          string
	HighestSequence int64
	Message         string
}

type ClaimFilter struct {
	PreferredExperimentID string
}

func TrialRequestHash(experimentID, logicalTrialID, trialID, pairID, arm string, attempt int) (string, error) {
	if experimentID == "" || logicalTrialID == "" || trialID == "" || attempt < 1 {
		return "", fmt.Errorf("trial request identity 不完整")
	}
	return identity.HashCanonical(map[string]any{
		"experiment_id":    experimentID,
		"logical_trial_id": logicalTrialID,
		"trial_id":         trialID,
		"pair_id":          pairID,
		"arm":              arm,
		"attempt_no":       attempt,
	})
}

func LogicalTrialID(pairID, arm string) (string, error) {
	if pairID == "" || arm == "" {
		return "", fmt.Errorf("pair_id 和 arm 不能为空")
	}
	return identity.HashCanonical(map[string]any{"pair_id": pairID, "arm": arm})
}

func ResultIdempotencyKey(trialID, resultManifestHash string) (string, error) {
	if trialID == "" || resultManifestHash == "" {
		return "", fmt.Errorf("trial_id 和 result manifest hash 不能为空")
	}
	return identity.HashCanonical(map[string]any{"trial_id": trialID, "result_manifest_hash": resultManifestHash})
}

func ResultManifestHash(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return "", fmt.Errorf("result manifest 大小必须位于 1..1048576 bytes")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("result manifest 必须是 JSON: %w", err)
	}
	return identity.HashCanonical(value)
}

func ValidOutcome(value Outcome) bool {
	switch value {
	case OutcomeSucceeded, OutcomeFailed, OutcomeTimedOut, OutcomeCancelled:
		return true
	default:
		return false
	}
}
