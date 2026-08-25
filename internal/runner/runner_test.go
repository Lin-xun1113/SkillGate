package runner_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// mockQueueStore implements store.QueueStore for unit tests.
type mockQueueStore struct {
	materializeFunc func(context.Context, *manifest.CompiledExperiment, scheduler.MaterializeOptions) (scheduler.MaterializeResult, error)
	claimFunc       func(context.Context, string, time.Duration) (scheduler.Claim, error)
	startFunc       func(context.Context, scheduler.Heartbeat) error
	heartbeatFunc   func(context.Context, scheduler.Heartbeat, time.Duration) error
	completeFunc    func(context.Context, scheduler.Completion) (scheduler.CommitResult, error)
	sweepFunc       func(context.Context, int) (scheduler.SweepResult, error)
	cancelFunc      func(context.Context, string, string, string) (scheduler.CancelResult, error)
}

func (m *mockQueueStore) Materialize(ctx context.Context, e *manifest.CompiledExperiment, opts scheduler.MaterializeOptions) (scheduler.MaterializeResult, error) {
	if m.materializeFunc != nil {
		return m.materializeFunc(ctx, e, opts)
	}
	return scheduler.MaterializeResult{}, nil
}

func (m *mockQueueStore) Claim(ctx context.Context, workerID string, d time.Duration) (scheduler.Claim, error) {
	if m.claimFunc != nil {
		return m.claimFunc(ctx, workerID, d)
	}
	return scheduler.Claim{}, &scheduler.Error{Code: scheduler.CodeNotClaimable}
}

func (m *mockQueueStore) Start(ctx context.Context, h scheduler.Heartbeat) error {
	if m.startFunc != nil {
		return m.startFunc(ctx, h)
	}
	return nil
}

func (m *mockQueueStore) Heartbeat(ctx context.Context, h scheduler.Heartbeat, d time.Duration) error {
	if m.heartbeatFunc != nil {
		return m.heartbeatFunc(ctx, h, d)
	}
	return nil
}

func (m *mockQueueStore) Complete(ctx context.Context, c scheduler.Completion) (scheduler.CommitResult, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, c)
	}
	return scheduler.CommitResult{LogicalTrialID: c.LogicalTrialID, TrialID: c.TrialID, ManifestHash: c.ManifestHash, Outcome: c.Outcome}, nil
}

func (m *mockQueueStore) SweepExpired(ctx context.Context, limit int) (scheduler.SweepResult, error) {
	if m.sweepFunc != nil {
		return m.sweepFunc(ctx, limit)
	}
	return scheduler.SweepResult{}, nil
}

func (m *mockQueueStore) CancelExperiment(ctx context.Context, expID, actor, reason string) (scheduler.CancelResult, error) {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, expID, actor, reason)
	}
	return scheduler.CancelResult{}, nil
}

func (m *mockQueueStore) Close() {}

func setupTestServer(t *testing.T, qStore *mockQueueStore) (*runner.Server, runnerv1.RunnerControlClient, func()) {
	t.Helper()
	server := runner.NewServer(qStore, nil)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go func() {
		_ = server.Serve(lis)
	}()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	client := runnerv1.NewRunnerControlClient(conn)

	cleanup := func() {
		_ = conn.Close()
		server.Stop()
	}
	return server, client, cleanup
}

func TestWorkerRegistration(t *testing.T) {
	mockStore := &mockQueueStore{}
	_, client, cleanup := setupTestServer(t, mockStore)
	defer cleanup()

	ctx := context.Background()

	// 1. Success registration
	regResp, err := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-01",
		ProtocolVersion: "runner.v1",
		WorkerVersion:   "0.1.0",
		Capabilities: &runnerv1.WorkerCapabilities{
			Harnesses: []string{"fixture"},
		},
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if !regResp.Accepted {
		t.Fatalf("expected accepted, got reject: %s", regResp.RejectReason)
	}
	if regResp.SessionToken == "" {
		t.Fatalf("expected non-empty session token")
	}

	// 2. Protocol version mismatch
	badResp, err := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-02",
		ProtocolVersion: "runner.v999",
	})
	if err != nil {
		t.Fatalf("unexpected grpc err: %v", err)
	}
	if badResp.Accepted {
		t.Fatalf("expected reject for unsupported protocol")
	}
}

func TestClaimAndRequestHashVerification(t *testing.T) {
	mockStore := &mockQueueStore{
		claimFunc: func(ctx context.Context, wID string, d time.Duration) (scheduler.Claim, error) {
			return scheduler.Claim{
				ExperimentID:    "exp-01",
				LogicalTrialID:  "logical-01",
				TrialID:         "trial-01",
				PairID:          "pair-01",
				Arm:             "with_skill",
				Attempt:         1,
				WorkerID:        wID,
				LeaseToken:      "lease-token-123",
				LeaseGeneration: 1,
				LeaseExpiresAt:  time.Now().Add(d),
			}, nil
		},
	}
	_, client, cleanup := setupTestServer(t, mockStore)
	defer cleanup()

	ctx := context.Background()
	reg, _ := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-01",
		ProtocolVersion: "runner.v1",
	})

	claimResp, err := client.ClaimTrial(ctx, &runnerv1.ClaimTrialRequest{
		WorkerId:     "worker-01",
		SessionToken: reg.SessionToken,
	})
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !claimResp.HasTrial {
		t.Fatalf("expected trial claimed")
	}
	if claimResp.TrialId != "trial-01" {
		t.Fatalf("expected trial-01, got %s", claimResp.TrialId)
	}

	// Verify request hash
	var parsedObj any
	if err := json.Unmarshal([]byte(claimResp.TrialRequestJson), &parsedObj); err != nil {
		t.Fatalf("failed to unmarshal trial request json: %v", err)
	}
	computedHash, err := identity.HashCanonical(parsedObj)
	if err != nil {
		t.Fatalf("failed to hash: %v", err)
	}
	if computedHash != claimResp.RequestHash {
		t.Fatalf("request hash mismatch: got %s, expected %s", claimResp.RequestHash, computedHash)
	}
}

func TestHeartbeatAndCancellation(t *testing.T) {
	cancelRequested := false
	mockStore := &mockQueueStore{
		heartbeatFunc: func(ctx context.Context, h scheduler.Heartbeat, d time.Duration) error {
			if cancelRequested {
				return &scheduler.Error{Code: scheduler.CodeExperimentCancelRequested}
			}
			return nil
		},
	}
	_, client, cleanup := setupTestServer(t, mockStore)
	defer cleanup()

	ctx := context.Background()
	reg, _ := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-01",
		ProtocolVersion: "runner.v1",
	})

	// Normal heartbeat
	hbResp, err := client.Heartbeat(ctx, &runnerv1.HeartbeatRequest{
		WorkerId:      "worker-01",
		SessionToken:  reg.SessionToken,
		TrialId:       "trial-01",
		LeaseToken:    "lease-123",
		Phase:         "running",
		EventSequence: 1,
	})
	if err != nil {
		t.Fatalf("heartbeat err: %v", err)
	}
	if hbResp.Action != runnerv1.HeartbeatAction_HEARTBEAT_ACTION_CONTINUE {
		t.Fatalf("expected CONTINUE, got %v", hbResp.Action)
	}

	// Trigger cancellation
	cancelRequested = true
	hbCancelResp, err := client.Heartbeat(ctx, &runnerv1.HeartbeatRequest{
		WorkerId:      "worker-01",
		SessionToken:  reg.SessionToken,
		TrialId:       "trial-01",
		LeaseToken:    "lease-123",
		Phase:         "running",
		EventSequence: 2,
	})
	if err != nil {
		t.Fatalf("heartbeat cancel err: %v", err)
	}
	if hbCancelResp.Action != runnerv1.HeartbeatAction_HEARTBEAT_ACTION_CANCEL_REQUESTED {
		t.Fatalf("expected CANCEL_REQUESTED, got %v", hbCancelResp.Action)
	}
}

func TestEventReportingAndDeduplication(t *testing.T) {
	mockStore := &mockQueueStore{}
	_, client, cleanup := setupTestServer(t, mockStore)
	defer cleanup()

	ctx := context.Background()
	payload := map[string]any{"tool": "read_file", "path": "test.csv"}
	payloadJSON, _ := json.Marshal(payload)
	payloadHash, _ := identity.HashCanonical(payload)

	// 1. First event report
	reg, err := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{WorkerId: "event-worker", ProtocolVersion: "runner.v1"})
	if err != nil || !reg.Accepted {
		t.Fatalf("register event worker failed: %v", err)
	}
	resp1, err := client.ReportEvent(ctx, &runnerv1.ReportEventRequest{
		EventId:          "evt-001",
		WorkerId:         "event-worker",
		SessionToken:     reg.SessionToken,
		LeaseToken:       "lease-123",
		TrialId:          "trial-01",
		LogicalTrialId:   "logical-01",
		ExperimentId:     "exp-01",
		Sequence:         1,
		EventType:        "tool_call.finished",
		OccurredAtUnixMs: time.Now().UnixMilli(),
		PayloadJson:      string(payloadJSON),
		PayloadHash:      payloadHash,
	})
	if err != nil {
		t.Fatalf("report event failed: %v", err)
	}
	if resp1.Status != runnerv1.EventReportStatus_EVENT_REPORT_STATUS_ACCEPTED {
		t.Fatalf("expected ACCEPTED, got %v", resp1.Status)
	}

	// 2. Duplicate event report with same event_id
	resp2, err := client.ReportEvent(ctx, &runnerv1.ReportEventRequest{
		EventId:        "evt-001",
		WorkerId:       "event-worker",
		SessionToken:   reg.SessionToken,
		LeaseToken:     "lease-123",
		TrialId:        "trial-01",
		LogicalTrialId: "logical-01",
		ExperimentId:   "exp-01",
		Sequence:       1,
		EventType:      "tool_call.finished",
		PayloadJson:    string(payloadJSON),
		PayloadHash:    payloadHash,
	})
	if err != nil {
		t.Fatalf("report duplicate event failed: %v", err)
	}
	if resp2.Status != runnerv1.EventReportStatus_EVENT_REPORT_STATUS_DUPLICATE_IGNORED {
		t.Fatalf("expected DUPLICATE_IGNORED, got %v", resp2.Status)
	}

	// 3. Sequence gap detection
	resp3, err := client.ReportEvent(ctx, &runnerv1.ReportEventRequest{
		EventId:        "evt-003",
		WorkerId:       "event-worker",
		SessionToken:   reg.SessionToken,
		LeaseToken:     "lease-123",
		TrialId:        "trial-01",
		LogicalTrialId: "logical-01",
		ExperimentId:   "exp-01",
		Sequence:       5, // gap from 1 to 5
		EventType:      "tool_call.finished",
	})
	if err != nil {
		t.Fatalf("report gap event failed: %v", err)
	}
	if resp3.Status != runnerv1.EventReportStatus_EVENT_REPORT_STATUS_SEQUENCE_GAP {
		t.Fatalf("expected SEQUENCE_GAP, got %v", resp3.Status)
	}
}

func TestCompleteTrialIdempotency(t *testing.T) {
	commits := 0
	mockStore := &mockQueueStore{
		completeFunc: func(ctx context.Context, c scheduler.Completion) (scheduler.CommitResult, error) {
			commits++
			if commits > 1 {
				return scheduler.CommitResult{
					LogicalTrialID: c.LogicalTrialID,
					TrialID:        c.TrialID,
					ResultID:       "result-01",
					ManifestHash:   c.ManifestHash,
					Outcome:        c.Outcome,
					Idempotent:     true,
				}, nil
			}
			return scheduler.CommitResult{
				LogicalTrialID: c.LogicalTrialID,
				TrialID:        c.TrialID,
				ResultID:       "result-01",
				ManifestHash:   c.ManifestHash,
				Outcome:        c.Outcome,
				Idempotent:     false,
			}, nil
		},
	}
	_, client, cleanup := setupTestServer(t, mockStore)
	defer cleanup()

	ctx := context.Background()
	reg, _ := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-01",
		ProtocolVersion: "runner.v1",
	})

	outcomeManifest := `{"answer": "42", "pass": true}`
	compReq := &runnerv1.CompleteTrialRequest{
		WorkerId:            "worker-01",
		SessionToken:        reg.SessionToken,
		TrialId:             "trial-01",
		LogicalTrialId:      "logical-01",
		AttemptNo:           1,
		LeaseToken:          "lease-123",
		RequestHash:         "request-hash",
		IdempotencyKey:      "idempotency-key",
		Outcome:             "SUCCEEDED",
		OutcomeManifestJson: outcomeManifest,
	}

	// 1st commit
	resp1, err := client.CompleteTrial(ctx, compReq)
	if err != nil {
		t.Fatalf("1st commit failed: %v", err)
	}
	if resp1.Status != runnerv1.CompletionStatus_COMPLETION_STATUS_COMMITTED {
		t.Fatalf("expected COMMITTED, got %v", resp1.Status)
	}

	// 2nd commit (duplicate)
	resp2, err := client.CompleteTrial(ctx, compReq)
	if err != nil {
		t.Fatalf("2nd commit failed: %v", err)
	}
	if resp2.Status != runnerv1.CompletionStatus_COMPLETION_STATUS_ALREADY_COMMITTED {
		t.Fatalf("expected ALREADY_COMMITTED, got %v", resp2.Status)
	}
}

func TestFailTrialRetry(t *testing.T) {
	mockStore := &mockQueueStore{
		completeFunc: func(ctx context.Context, c scheduler.Completion) (scheduler.CommitResult, error) {
			return scheduler.CommitResult{
				LogicalTrialID: c.LogicalTrialID,
				TrialID:        c.TrialID,
				Outcome:        c.Outcome,
				RetryScheduled: true,
				NextTrialID:    "trial-01-attempt-2",
			}, nil
		},
	}
	_, client, cleanup := setupTestServer(t, mockStore)
	defer cleanup()

	ctx := context.Background()
	reg, _ := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-01",
		ProtocolVersion: "runner.v1",
	})

	failResp, err := client.FailTrial(ctx, &runnerv1.FailTrialRequest{
		WorkerId:        "worker-01",
		SessionToken:    reg.SessionToken,
		TrialId:         "trial-01",
		LogicalTrialId:  "logical-01",
		AttemptNo:       1,
		LeaseToken:      "lease-123",
		RequestHash:     "request-hash",
		FailureCategory: runnerv1.FailureCategory_FAILURE_CATEGORY_TRANSIENT,
		ErrorMessage:    "temporary network failure",
	})
	if err != nil {
		t.Fatalf("fail trial request failed: %v", err)
	}
	if failResp.Status != runnerv1.FailTrialStatus_FAIL_TRIAL_STATUS_RETRY_SCHEDULED {
		t.Fatalf("expected RETRY_SCHEDULED, got %v", failResp.Status)
	}
	if failResp.NextAttemptNo != 2 {
		t.Fatalf("expected next attempt no 2, got %d", failResp.NextAttemptNo)
	}
}
