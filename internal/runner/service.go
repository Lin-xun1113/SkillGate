package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/Lin-xun1113/SkillGate/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	SupportedProtocolVersion = "runner.v1"
	DefaultHeartbeatInterval = 3000  // 3s
	DefaultMaxLeaseMS        = 30000 // 30s
)

type ServiceOptions struct {
	SupportedProtocols []string
	HeartbeatInterval  time.Duration
	MaxLeaseDuration   time.Duration
	Projector          ExecutionProjector // M4: optional execution content projector
	ArtifactsRoot      string             // Root directory for artifact storage validation
}

// ExecutionProjector resolves manifest_hash to execution content.
type ExecutionProjector interface {
	Project(ctx context.Context, manifestHash, pairID, arm string) (*ExecutionSpec, string, error)
}

type RunnerService struct {
	runnerv1.UnimplementedRunnerControlServer
	store     store.QueueStore
	sessions  *SessionManager
	events    *EventManager
	opts      ServiceOptions
	projector ExecutionProjector // M4: nil for M3-only deployments
}

func NewRunnerService(qStore store.QueueStore, opts *ServiceOptions) *RunnerService {
	opt := ServiceOptions{
		SupportedProtocols: []string{SupportedProtocolVersion},
		HeartbeatInterval:  3 * time.Second,
		MaxLeaseDuration:   30 * time.Second,
	}
	if opts != nil {
		if len(opts.SupportedProtocols) > 0 {
			opt.SupportedProtocols = opts.SupportedProtocols
		}
		if opts.HeartbeatInterval > 0 {
			opt.HeartbeatInterval = opts.HeartbeatInterval
		}
		if opts.MaxLeaseDuration > 0 {
			opt.MaxLeaseDuration = opts.MaxLeaseDuration
		}
		opt.Projector = opts.Projector
	}
	return &RunnerService{
		store:     qStore,
		sessions:  NewSessionManager(),
		events:    NewEventManager(),
		opts:      opt,
		projector: opt.Projector,
	}
}

func (s *RunnerService) EventManager() *EventManager {
	return s.events
}

func (s *RunnerService) SessionManager() *SessionManager {
	return s.sessions
}

func (s *RunnerService) RegisterWorker(ctx context.Context, req *runnerv1.RegisterWorkerRequest) (*runnerv1.RegisterWorkerResponse, error) {
	if req.WorkerId == "" {
		return &runnerv1.RegisterWorkerResponse{
			Accepted:     false,
			RejectReason: "worker_id is required",
		}, nil
	}

	supported := false
	for _, p := range s.opts.SupportedProtocols {
		if req.ProtocolVersion == p {
			supported = true
			break
		}
	}
	if !supported {
		return &runnerv1.RegisterWorkerResponse{
			Accepted:              false,
			ServerProtocolVersion: SupportedProtocolVersion,
			RejectReason:          fmt.Sprintf("PROTOCOL_UNSUPPORTED: %s is not supported by server (expected %s)", req.ProtocolVersion, SupportedProtocolVersion),
		}, nil
	}

	sess, err := s.sessions.Register(req)
	if err != nil {
		return &runnerv1.RegisterWorkerResponse{
			Accepted:              false,
			ServerProtocolVersion: SupportedProtocolVersion,
			RejectReason:          err.Error(),
		}, nil
	}

	return &runnerv1.RegisterWorkerResponse{
		Accepted:              true,
		SessionToken:          sess.SessionToken,
		HeartbeatIntervalMs:   s.opts.HeartbeatInterval.Milliseconds(),
		MaxLeaseMs:            s.opts.MaxLeaseDuration.Milliseconds(),
		ServerProtocolVersion: SupportedProtocolVersion,
	}, nil
}

func (s *RunnerService) ClaimTrial(ctx context.Context, req *runnerv1.ClaimTrialRequest) (*runnerv1.ClaimTrialResponse, error) {
	if req.WorkerId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "worker_id is required")
	}
	if !s.sessions.Validate(req.WorkerId, req.SessionToken) {
		return nil, status.Errorf(codes.Unauthenticated, "invalid or expired session_token for worker %s", req.WorkerId)
	}

	leaseDuration := s.opts.MaxLeaseDuration
	if req.LeaseDurationSec > 0 {
		leaseDuration = time.Duration(req.LeaseDurationSec) * time.Second
	}
	if leaseDuration > s.opts.MaxLeaseDuration {
		leaseDuration = s.opts.MaxLeaseDuration
	}

	var claim scheduler.Claim
	var err error
	if filtered, ok := s.store.(store.FilteredClaimStore); ok {
		claim, err = filtered.ClaimFiltered(ctx, req.WorkerId, leaseDuration, scheduler.ClaimFilter{PreferredExperimentID: req.PreferredExperimentId})
	} else {
		claim, err = s.store.Claim(ctx, req.WorkerId, leaseDuration)
	}
	if err != nil {
		if scheduler.CodeOf(err) == scheduler.CodeNotClaimable {
			return &runnerv1.ClaimTrialResponse{
				HasTrial: false,
			}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to claim trial: %v", err)
	}

	reqJSON, reqHash, err := BuildTrialRequestPayload(claim)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build trial request: %v", err)
	}

	// M4: Project execution content if projector is configured
	if s.projector != nil && claim.ManifestHash != "" {
		execSpec, execHash, err := s.projector.Project(ctx, claim.ManifestHash, claim.PairID, claim.Arm)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to project execution: %v", err)
		}

		// Use new ProjectExecution that returns all components
		reqJSON, reqHash, _, err = ProjectExecution(
			reqJSON,
			reqHash,
			claim.ManifestHash,
			execSpec.CaseID,
			claim.Arm, // arm parameter
			execSpec.CaseInput,
			execSpec.SkillHash,
			execSpec.Model,
			execSpec.ToolPolicy,
			execSpec.Environment,
		)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to inject execution: %v", err)
		}
		_ = execHash // execHash from projector is redundant; ProjectExecution recomputes it
	}

	return &runnerv1.ClaimTrialResponse{
		HasTrial:             true,
		TrialId:              claim.TrialID,
		LogicalTrialId:       claim.LogicalTrialID,
		ExperimentId:         claim.ExperimentID,
		PairId:               claim.PairID,
		Arm:                  claim.Arm,
		AttemptNo:            int32(claim.Attempt),
		LeaseToken:           claim.LeaseToken,
		LeaseGeneration:      claim.LeaseGeneration,
		LeaseExpiresAtUnixMs: claim.LeaseExpiresAt.UnixMilli(),
		RequestHash:          reqHash,
		TrialRequestJson:     reqJSON,
	}, nil
}

func (s *RunnerService) Heartbeat(ctx context.Context, req *runnerv1.HeartbeatRequest) (*runnerv1.HeartbeatResponse, error) {
	if req.WorkerId == "" || req.TrialId == "" || req.LeaseToken == "" {
		return nil, status.Errorf(codes.InvalidArgument, "worker_id, trial_id and lease_token are required")
	}
	if !s.sessions.Validate(req.WorkerId, req.SessionToken) {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session_token")
	}

	s.sessions.TouchHeartbeat(req.WorkerId)

	hb := scheduler.Heartbeat{
		TrialID:         req.TrialId,
		WorkerID:        req.WorkerId,
		LeaseToken:      req.LeaseToken,
		LeaseGeneration: req.LeaseGeneration,
		EventSequence:   req.EventSequence,
		Phase:           req.Phase,
		Usage: scheduler.ResourceUsage{
			InputTokens: req.ResourceUsage.GetInputTokens(), OutputTokens: req.ResourceUsage.GetOutputTokens(),
			ElapsedMS: req.ResourceUsage.GetElapsedMs(), ToolCalls: req.ResourceUsage.GetToolCalls(), PeakMemoryBytes: req.ResourceUsage.GetPeakMemoryBytes(),
		},
	}

	// The first worker heartbeat starts a newly claimed attempt. Later
	// heartbeats renew it. PostgreSQL exposes the actual renewed expiration
	// through the optional result interface.
	leaseExpiresAt := time.Now().Add(s.opts.MaxLeaseDuration)
	var err error
	if req.Phase == "start" {
		err = s.store.Start(ctx, hb)
	} else if detailed, ok := s.store.(store.HeartbeatResultStore); ok {
		result, detailedErr := detailed.HeartbeatWithResult(ctx, hb, s.opts.MaxLeaseDuration)
		err = detailedErr
		if detailedErr == nil {
			leaseExpiresAt = result.LeaseExpiresAt
		}
	} else {
		err = s.store.Heartbeat(ctx, hb, s.opts.MaxLeaseDuration)
	}
	if err != nil {
		code := scheduler.CodeOf(err)
		switch code {
		case scheduler.CodeExperimentCancelRequested:
			return &runnerv1.HeartbeatResponse{
				Action:  runnerv1.HeartbeatAction_HEARTBEAT_ACTION_CANCEL_REQUESTED,
				Message: "experiment cancel requested",
			}, nil
		case scheduler.CodeLeaseExpired, scheduler.CodeLeaseMismatch, scheduler.CodeOwnerMismatch:
			return &runnerv1.HeartbeatResponse{
				Action:  runnerv1.HeartbeatAction_HEARTBEAT_ACTION_LEASE_REJECTED,
				Message: string(code),
			}, nil
		case scheduler.CodeStatusConflict:
			// Might need Start() if was never started
			startErr := s.store.Start(ctx, hb)
			if startErr == nil {
				return &runnerv1.HeartbeatResponse{
					Action:               runnerv1.HeartbeatAction_HEARTBEAT_ACTION_CONTINUE,
					LeaseExpiresAtUnixMs: leaseExpiresAt.UnixMilli(),
					Message:              "trial started",
				}, nil
			}
			return &runnerv1.HeartbeatResponse{
				Action:  runnerv1.HeartbeatAction_HEARTBEAT_ACTION_LEASE_REJECTED,
				Message: fmt.Sprintf("status conflict: %v", err),
			}, nil
		default:
			return nil, status.Errorf(codes.Internal, "heartbeat failed: %v", err)
		}
	}

	return &runnerv1.HeartbeatResponse{
		Action:               runnerv1.HeartbeatAction_HEARTBEAT_ACTION_CONTINUE,
		LeaseExpiresAtUnixMs: leaseExpiresAt.UnixMilli(),
		Message:              "lease renewed",
	}, nil
}

func (s *RunnerService) ReportEvent(ctx context.Context, req *runnerv1.ReportEventRequest) (*runnerv1.ReportEventResponse, error) {
	if req.WorkerId == "" || req.SessionToken == "" || req.LeaseToken == "" {
		return nil, status.Error(codes.Unauthenticated, "worker_id, session_token and lease_token are required")
	}
	if !s.sessions.Validate(req.WorkerId, req.SessionToken) {
		return nil, status.Error(codes.Unauthenticated, "invalid session_token")
	}
	if persistent, ok := s.store.(store.EventStore); ok {
		result, err := persistent.ReportEvent(ctx, scheduler.Event{
			EventID: req.EventId, TrialID: req.TrialId, LogicalTrialID: req.LogicalTrialId,
			ExperimentID: req.ExperimentId, AttemptNo: int(req.AttemptNo), WorkerID: req.WorkerId,
			LeaseToken: req.LeaseToken, LeaseGeneration: req.LeaseGeneration, Sequence: req.Sequence,
			EventType: req.EventType, OccurredAt: time.UnixMilli(req.OccurredAtUnixMs).UTC(),
			Payload: []byte(req.PayloadJson), PayloadHash: req.PayloadHash,
		})
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to record event: %v", err)
		}
		return &runnerv1.ReportEventResponse{Status: eventStatus(result.Status), HighestSequence: result.HighestSequence, Message: result.Message}, nil
	}
	statusEnum, highestSeq, msg, err := s.events.RecordEvent(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to record event: %v", err)
	}
	return &runnerv1.ReportEventResponse{Status: statusEnum, HighestSequence: highestSeq, Message: msg}, nil
}

func eventStatus(value string) runnerv1.EventReportStatus {
	switch value {
	case "DUPLICATE_IGNORED":
		return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_DUPLICATE_IGNORED
	case "SEQUENCE_GAP":
		return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_SEQUENCE_GAP
	case "REJECTED":
		return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_REJECTED
	default:
		return runnerv1.EventReportStatus_EVENT_REPORT_STATUS_ACCEPTED
	}
}

func (s *RunnerService) CompleteTrial(ctx context.Context, req *runnerv1.CompleteTrialRequest) (*runnerv1.CompleteTrialResponse, error) {
	if req.WorkerId == "" || req.TrialId == "" || req.LogicalTrialId == "" || req.LeaseToken == "" {
		return &runnerv1.CompleteTrialResponse{
			Status:  runnerv1.CompletionStatus_COMPLETION_STATUS_INVALID_REQUEST,
			Message: "missing required trial completion fields",
		}, nil
	}
	if !s.sessions.Validate(req.WorkerId, req.SessionToken) {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session_token")
	}

	outcome := parseOutcome(req.Outcome)
	manifestBytes := []byte(req.OutcomeManifestJson)
	if len(manifestBytes) == 0 {
		manifestBytes = []byte("{}")
	}
	if req.RequestHash == "" || req.IdempotencyKey == "" {
		return &runnerv1.CompleteTrialResponse{Status: runnerv1.CompletionStatus_COMPLETION_STATUS_INVALID_REQUEST, Message: "request_hash and idempotency_key are required"}, nil
	}
	if err := s.validateArtifactManifests(req.Artifacts); err != nil {
		return &runnerv1.CompleteTrialResponse{Status: runnerv1.CompletionStatus_COMPLETION_STATUS_INVALID_REQUEST, Message: err.Error()}, nil
	}
	manifestHash, err := scheduler.ResultManifestHash(manifestBytes)
	if err != nil {
		return &runnerv1.CompleteTrialResponse{
			Status:  runnerv1.CompletionStatus_COMPLETION_STATUS_INVALID_REQUEST,
			Message: fmt.Sprintf("invalid outcome manifest json: %v", err),
		}, nil
	}
	artifactsJSON, _ := json.Marshal(req.Artifacts)
	usageJSON, _ := json.Marshal(req.Usage)
	gradesJSON := jsonOrEmpty(req.GradesJson)
	if gradesJSON == nil {
		return &runnerv1.CompleteTrialResponse{Status: runnerv1.CompletionStatus_COMPLETION_STATUS_INVALID_REQUEST, Message: "grades_json must be valid JSON"}, nil
	}

	completion := scheduler.Completion{
		LogicalTrialID:  req.LogicalTrialId,
		TrialID:         req.TrialId,
		WorkerID:        req.WorkerId,
		LeaseToken:      req.LeaseToken,
		LeaseGeneration: req.LeaseGeneration,
		RequestHash:     req.RequestHash,
		IdempotencyKey:  req.IdempotencyKey,
		ManifestHash:    manifestHash,
		Manifest:        manifestBytes,
		Artifacts:       artifactsJSON,
		Usage:           usageJSON,
		Grades:          gradesJSON,
		Outcome:         outcome,
		EventSequence:   req.FinalSequence,
	}

	res, err := s.store.Complete(ctx, completion)
	if err != nil {
		code := scheduler.CodeOf(err)
		switch code {
		case scheduler.CodeLeaseExpired:
			return &runnerv1.CompleteTrialResponse{
				Status:  runnerv1.CompletionStatus_COMPLETION_STATUS_LEASE_EXPIRED,
				Message: "lease expired before completion",
			}, nil
		case scheduler.CodeResultConflict, scheduler.CodeIdentityConflict, scheduler.CodeLogicalTrialTerminal:
			return &runnerv1.CompleteTrialResponse{
				Status:  runnerv1.CompletionStatus_COMPLETION_STATUS_CONFLICT,
				Message: string(code),
			}, nil
		case scheduler.CodeInvalidArgument:
			return &runnerv1.CompleteTrialResponse{
				Status:  runnerv1.CompletionStatus_COMPLETION_STATUS_INVALID_REQUEST,
				Message: err.Error(),
			}, nil
		default:
			return nil, status.Errorf(codes.Internal, "completion failed: %v", err)
		}
	}

	respStatus := runnerv1.CompletionStatus_COMPLETION_STATUS_COMMITTED
	if res.Idempotent {
		respStatus = runnerv1.CompletionStatus_COMPLETION_STATUS_ALREADY_COMMITTED
	}

	return &runnerv1.CompleteTrialResponse{
		Status:   respStatus,
		ResultId: res.ResultID,
		Message:  "trial outcome committed successfully",
	}, nil
}

func (s *RunnerService) FailTrial(ctx context.Context, req *runnerv1.FailTrialRequest) (*runnerv1.FailTrialResponse, error) {
	if req.WorkerId == "" || req.TrialId == "" || req.LogicalTrialId == "" || req.LeaseToken == "" {
		return &runnerv1.FailTrialResponse{
			Status:  runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_REJECTED,
			Message: "missing required fail fields",
		}, nil
	}
	if !s.sessions.Validate(req.WorkerId, req.SessionToken) {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session_token")
	}

	manifestBytes := []byte(req.OutcomeManifestJson)
	if len(manifestBytes) == 0 {
		manifestBytes = []byte(fmt.Sprintf(`{"error": %q, "category": %q}`, req.ErrorMessage, req.FailureCategory.String()))
	}
	if req.RequestHash == "" {
		return &runnerv1.FailTrialResponse{Status: runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_REJECTED, Message: "request_hash is required"}, nil
	}
	manifestHash, err := scheduler.ResultManifestHash(manifestBytes)
	if err != nil {
		return &runnerv1.FailTrialResponse{
			Status:  runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_REJECTED,
			Message: fmt.Sprintf("invalid outcome manifest: %v", err),
		}, nil
	}

	cat := mapFailureCategory(req.FailureCategory)
	completion := scheduler.Completion{
		LogicalTrialID:  req.LogicalTrialId,
		TrialID:         req.TrialId,
		WorkerID:        req.WorkerId,
		LeaseToken:      req.LeaseToken,
		LeaseGeneration: req.LeaseGeneration,
		RequestHash:     req.RequestHash,
		ManifestHash:    manifestHash,
		Manifest:        manifestBytes,
		Outcome:         scheduler.OutcomeFailed,
		Category:        cat,
		EventSequence:   req.EventSequence,
	}

	res, err := s.store.Complete(ctx, completion)
	if err != nil {
		code := scheduler.CodeOf(err)
		switch code {
		case scheduler.CodeLeaseExpired:
			return &runnerv1.FailTrialResponse{
				Status:  runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_LEASE_EXPIRED,
				Message: "lease expired before fail report",
			}, nil
		default:
			return &runnerv1.FailTrialResponse{
				Status:  runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_REJECTED,
				Message: fmt.Sprintf("fail trial rejected: %v", err),
			}, nil
		}
	}

	if res.RetryScheduled {
		return &runnerv1.FailTrialResponse{
			Status:        runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_RETRY_SCHEDULED,
			NextAttemptNo: int32(req.AttemptNo + 1),
			Message:       fmt.Sprintf("retry scheduled: %s", res.NextTrialID),
		}, nil
	}

	return &runnerv1.FailTrialResponse{
		Status:  runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_TERMINAL_FAILED,
		Message: "trial terminal failed",
	}, nil
}

func parseOutcome(out string) scheduler.Outcome {
	switch strings.ToUpper(strings.TrimSpace(out)) {
	case "SUCCEEDED":
		return scheduler.OutcomeSucceeded
	case "TIMED_OUT":
		return scheduler.OutcomeTimedOut
	case "CANCELLED":
		return scheduler.OutcomeCancelled
	default:
		return scheduler.OutcomeFailed
	}
}

func (s *RunnerService) validateArtifactManifests(artifacts []*runnerv1.ArtifactManifest) error {
	for _, artifact := range artifacts {
		if artifact == nil || artifact.ArtifactId == "" || artifact.Sha256 == "" || artifact.SizeBytes < 0 {
			return fmt.Errorf("artifact manifest requires artifact_id, sha256 and non-negative size_bytes")
		}
		if artifact.LocalPath == "" {
			continue
		}

		// Validate path is within artifacts root
		if s.opts.ArtifactsRoot != "" {
			if ok, err := isWithinArtifactsRoot(artifact.LocalPath, s.opts.ArtifactsRoot); !ok {
				return fmt.Errorf("artifact %s path validation failed: %w", artifact.ArtifactId, err)
			}
		}

		file, err := os.Open(artifact.LocalPath)
		if err != nil {
			return fmt.Errorf("artifact %s cannot be opened: %w", artifact.ArtifactId, err)
		}
		stat, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return fmt.Errorf("artifact %s stat failed: %w", artifact.ArtifactId, err)
		}
		if stat.Size() != artifact.SizeBytes {
			_ = file.Close()
			return fmt.Errorf("artifact %s size mismatch", artifact.ArtifactId)
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			_ = file.Close()
			return fmt.Errorf("artifact %s hash failed: %w", artifact.ArtifactId, err)
		}
		_ = file.Close()
		actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
		if actual != artifact.Sha256 {
			return fmt.Errorf("artifact %s hash mismatch", artifact.ArtifactId)
		}
	}
	return nil
}

// isWithinArtifactsRoot checks if the given path is within the allowed artifacts root.
// It resolves symlinks, cleans paths, and rejects any path traversal attempts.
func isWithinArtifactsRoot(targetPath, artifactsRoot string) (bool, error) {
	if targetPath == "" {
		return false, fmt.Errorf("target path is empty")
	}
	if artifactsRoot == "" {
		return false, fmt.Errorf("artifacts root is not configured")
	}

	// Resolve and clean the artifacts root
	cleanRoot, err := filepath.EvalSymlinks(artifactsRoot)
	if err != nil {
		cleanRoot = filepath.Clean(artifactsRoot)
	}
	cleanRoot = filepath.Clean(cleanRoot)
	if !strings.HasSuffix(cleanRoot, string(filepath.Separator)) {
		cleanRoot += string(filepath.Separator)
	}

	// Build the full path
	var fullPath string
	if filepath.IsAbs(targetPath) {
		fullPath = targetPath
	} else {
		fullPath = filepath.Join(artifactsRoot, targetPath)
	}

	// Clean and resolve the target path
	cleanTarget := filepath.Clean(fullPath)

	// Resolve symlinks if the path exists
	if resolved, err := filepath.EvalSymlinks(fullPath); err == nil {
		cleanTarget = resolved
	} else {
		// If path doesn't exist, resolve as much of the path as possible
		// Walk up the directory tree until we find an existing ancestor
		checkPath := fullPath
		for {
			parentDir := filepath.Dir(checkPath)
			if parentDir == checkPath || parentDir == "." || parentDir == "/" {
				break
			}

			if resolvedParent, err := filepath.EvalSymlinks(parentDir); err == nil {
				// Found an existing ancestor, rebuild the path with it
				relPath, err := filepath.Rel(parentDir, fullPath)
				if err == nil {
					cleanTarget = filepath.Join(resolvedParent, relPath)
					cleanTarget = filepath.Clean(cleanTarget)
				}
				break
			}

			checkPath = parentDir
		}
	}

	// Ensure the resolved path starts with the resolved root
	if !strings.HasPrefix(cleanTarget, cleanRoot) {
		return false, fmt.Errorf("path escapes artifacts root: %s", targetPath)
	}

	return true, nil
}

func jsonOrEmpty(raw string) []byte {
	if raw == "" {
		return []byte("{}")
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return nil
	}
	canonical, _ := json.Marshal(value)
	return canonical
}

func mapFailureCategory(cat runnerv1.FailureCategory) retry.Category {
	switch cat {
	case runnerv1.FailureCategory_FAILURE_CATEGORY_TRANSIENT:
		return retry.ProviderTransient
	case runnerv1.FailureCategory_FAILURE_CATEGORY_TIMEOUT:
		return retry.LeaseTimeout
	case runnerv1.FailureCategory_FAILURE_CATEGORY_SANDBOX_ERROR:
		return retry.DependencyTransient
	case runnerv1.FailureCategory_FAILURE_CATEGORY_INTERNAL:
		return retry.WorkerLost
	default:
		return retry.DeterministicFailure
	}
}
