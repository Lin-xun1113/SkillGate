package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func mustResultIdempotencyKey(t *testing.T, trialID, raw string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	key, err := scheduler.ResultIdempotencyKey(trialID, hash)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func setupTestPostgres(t *testing.T) (string, *pgxpool.Pool, string, func()) {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("需要 TEST_DATABASE_URL 指向真实 PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	schema := fmt.Sprintf("skillgate_m3_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		pool.Close()
		cancel()
		t.Fatal(err)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	databaseURL := parsed.String()

	// Run migrations
	sqlDB, err := postgres.OpenSQL(databaseURL)
	if err != nil {
		cancel()
		t.Fatalf("failed to open sql db: %v", err)
	}
	if err := postgres.Migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		cancel()
		t.Fatalf("migration failed: %v", err)
	}
	_ = sqlDB.Close()

	cleanup := func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = pool.Exec(cleanupCtx, `DROP SCHEMA IF EXISTS `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		pool.Close()
		cancel()
	}

	return databaseURL, pool, schema, cleanup
}

func TestM3RunnerProtocolWithPythonFixtureWorker(t *testing.T) {
	databaseURL, pool, schema, cleanup := setupTestPostgres(t)
	defer cleanup()

	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("failed to open postgres store: %v", err)
	}
	defer store.Close()

	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	compiled, diagnostics := manifest.Compile(manifestPath, root)
	if len(diagnostics) > 0 {
		t.Fatalf("compile diagnostics: %v", diagnostics)
	}
	opts := scheduler.MaterializeOptions{
		MaxAttempts:   2,
		Timeout:       10 * time.Second,
		BudgetTimeout: 5 * time.Minute,
		BackoffBase:   50 * time.Millisecond,
		BackoffCap:    time.Second,
		Retryable:     []retry.Category{retry.ProviderTransient},
	}
	matResult, err := store.Materialize(ctx, compiled, opts)
	if err != nil {
		t.Fatalf("materialize failed: %v", err)
	}
	if matResult.LogicalTrials != 48 {
		t.Fatalf("expected 48 logical trials, got %d", matResult.LogicalTrials)
	}

	server := runner.NewServer(store, &runner.ServiceOptions{
		HeartbeatInterval: time.Second,
		MaxLeaseDuration:  10 * time.Second,
	})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	serverAddr := lis.Addr().String()
	pythonBin := filepath.Join(root, "workers", "python", ".venv", "bin", "python3")
	if _, err := os.Stat(pythonBin); os.IsNotExist(err) {
		pythonBin = "python3"
	}

	workerScript := filepath.Join(root, "workers", "python", "fixture_worker", "worker.py")
	cmd := exec.CommandContext(ctx, pythonBin, workerScript, "--server", serverAddr, "--worker-id", "test-py-worker-01", "--max-trials", "48", "--delay", "0.002")
	cmd.Env = append(os.Environ(),
		"PYTHONPATH="+filepath.Join(root, "workers", "python")+":"+filepath.Join(root, "workers", "python", "gen"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python fixture worker failed: %v, output: %s", err, string(output))
	}

	var status string
	var total, terminal, succeeded, failed int
	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT status, total_logical_trials, terminal_count, succeeded_count, failed_count FROM %s.experiments WHERE experiment_id=$1`, schema), matResult.ExperimentID).Scan(&status, &total, &terminal, &succeeded, &failed)
	if err != nil {
		t.Fatalf("failed to query experiment: %v", err)
	}

	if total != 48 || terminal != 48 || succeeded != 48 || failed != 0 {
		t.Fatalf("expected all 48 succeeded, got total=%d terminal=%d succeeded=%d failed=%d", total, terminal, succeeded, failed)
	}
	if status != "GRADING" {
		t.Fatalf("expected status GRADING on full completion, got %s", status)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.trial_events`, schema)).Scan(&eventCount); err != nil {
		t.Fatalf("failed to query persisted events: %v", err)
	}
	if eventCount != 96 {
		t.Fatalf("expected 96 persisted events, got %d", eventCount)
	}

	// A6: every committed result must carry an artifact manifest and usage.
	var resultsWithoutArtifact, resultsWithoutUsage int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.trial_results WHERE jsonb_array_length(artifacts) = 0`, schema)).Scan(&resultsWithoutArtifact); err != nil {
		t.Fatalf("failed to query results without artifacts: %v", err)
	}
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.trial_results WHERE usage = '{}'::jsonb`, schema)).Scan(&resultsWithoutUsage); err != nil {
		t.Fatalf("failed to query results without usage: %v", err)
	}
	if resultsWithoutArtifact != 0 || resultsWithoutUsage != 0 {
		t.Fatalf("expected artifact and usage on every result, got without_artifact=%d without_usage=%d", resultsWithoutArtifact, resultsWithoutUsage)
	}
}

func TestM3ConcurrentPythonWorkers(t *testing.T) {
	databaseURL, pool, schema, cleanup := setupTestPostgres(t)
	defer cleanup()

	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("failed to open postgres store: %v", err)
	}
	defer store.Close()

	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	compiled, _ := manifest.Compile(manifestPath, root)
	matResult, err := store.Materialize(ctx, compiled, scheduler.MaterializeOptions{
		MaxAttempts:   2,
		Timeout:       10 * time.Second,
		BudgetTimeout: 5 * time.Minute,
		BackoffBase:   50 * time.Millisecond,
		BackoffCap:    time.Second,
	})
	if err != nil {
		t.Fatalf("materialize failed: %v", err)
	}

	server := runner.NewServer(store, &runner.ServiceOptions{
		HeartbeatInterval: time.Second,
		MaxLeaseDuration:  10 * time.Second,
	})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	serverAddr := lis.Addr().String()
	pythonBin := filepath.Join(root, "workers", "python", ".venv", "bin", "python3")
	workerScript := filepath.Join(root, "workers", "python", "fixture_worker", "worker.py")

	var wg sync.WaitGroup
	workers := 4
	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, pythonBin, workerScript, "--server", serverAddr, "--worker-id", fmt.Sprintf("py-worker-%02d", workerIndex), "--max-trials", "12", "--delay", "0.002")
			cmd.Env = append(os.Environ(),
				"PYTHONPATH="+filepath.Join(root, "workers", "python")+":"+filepath.Join(root, "workers", "python", "gen"),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("worker %d failed: %v, out: %s", workerIndex, err, string(out))
			}
		}(i)
	}
	wg.Wait()

	var terminal, succeeded int
	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT terminal_count, succeeded_count FROM %s.experiments WHERE experiment_id=$1`, schema), matResult.ExperimentID).Scan(&terminal, &succeeded)
	if err != nil {
		t.Fatalf("failed to query experiment: %v", err)
	}
	if terminal != 48 || succeeded != 48 {
		t.Fatalf("expected 48/48 from concurrent workers, got terminal=%d succeeded=%d", terminal, succeeded)
	}
}

func TestM3CancellationNotificationToWorker(t *testing.T) {
	databaseURL, pool, schema, cleanup := setupTestPostgres(t)
	defer cleanup()

	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("failed to open postgres store: %v", err)
	}
	defer store.Close()

	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "experiments", "csv-analysis-v1-demo.yaml")
	compiled, _ := manifest.Compile(manifestPath, root)
	matResult, err := store.Materialize(ctx, compiled, scheduler.MaterializeOptions{
		MaxAttempts:   2,
		Timeout:       10 * time.Second,
		BudgetTimeout: 5 * time.Minute,
		BackoffBase:   50 * time.Millisecond,
		BackoffCap:    time.Second,
	})
	if err != nil {
		t.Fatalf("materialize failed: %v", err)
	}

	server := runner.NewServer(store, &runner.ServiceOptions{
		HeartbeatInterval: 100 * time.Millisecond,
		MaxLeaseDuration:  5 * time.Second,
	})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	serverAddr := lis.Addr().String()

	// Connect gRPC client to simulate Worker lifecycle with Cancellation
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	client := runnerv1.NewRunnerControlClient(conn)

	regResp, err := client.RegisterWorker(ctx, &runnerv1.RegisterWorkerRequest{
		WorkerId:        "cancel-test-worker",
		ProtocolVersion: "runner.v1",
	})
	if err != nil || !regResp.Accepted {
		t.Fatalf("register failed: %v", err)
	}

	// 1. Claim a trial while experiment is RUNNING
	claimResp, err := client.ClaimTrial(ctx, &runnerv1.ClaimTrialRequest{
		WorkerId:     "cancel-test-worker",
		SessionToken: regResp.SessionToken,
	})
	if err != nil || !claimResp.HasTrial {
		t.Fatalf("claim failed: %v", err)
	}

	// 2. Request cancellation for the entire experiment
	_, err = store.CancelExperiment(ctx, matResult.ExperimentID, "test-suite", "user cancellation test")
	if err != nil {
		t.Fatalf("cancel experiment failed: %v", err)
	}

	// 3. Worker sends Heartbeat; must receive HEARTBEAT_ACTION_CANCEL_REQUESTED
	hbResp, err := client.Heartbeat(ctx, &runnerv1.HeartbeatRequest{
		WorkerId:        "cancel-test-worker",
		SessionToken:    regResp.SessionToken,
		TrialId:         claimResp.TrialId,
		LeaseToken:      claimResp.LeaseToken,
		LeaseGeneration: claimResp.LeaseGeneration,
		Phase:           "running",
		EventSequence:   1,
	})
	if err != nil {
		t.Fatalf("heartbeat err: %v", err)
	}
	if hbResp.Action != runnerv1.HeartbeatAction_HEARTBEAT_ACTION_CANCEL_REQUESTED {
		t.Fatalf("expected CANCEL_REQUESTED, got %v", hbResp.Action)
	}

	// 4. Worker complies and completes with outcome=CANCELLED
	compResp, err := client.CompleteTrial(ctx, &runnerv1.CompleteTrialRequest{
		WorkerId:            "cancel-test-worker",
		SessionToken:        regResp.SessionToken,
		TrialId:             claimResp.TrialId,
		LogicalTrialId:      claimResp.LogicalTrialId,
		AttemptNo:           claimResp.AttemptNo,
		LeaseToken:          claimResp.LeaseToken,
		LeaseGeneration:     claimResp.LeaseGeneration,
		RequestHash:         claimResp.RequestHash,
		IdempotencyKey:      mustResultIdempotencyKey(t, claimResp.TrialId, `{"cancelled": true, "reason": "user requested"}`),
		Outcome:             "CANCELLED",
		OutcomeManifestJson: `{"cancelled": true, "reason": "user requested"}`,
		FinalSequence:       2,
	})
	if err != nil {
		t.Fatalf("complete cancelled trial err: %v", err)
	}
	if compResp.Status != runnerv1.CompletionStatus_COMPLETION_STATUS_COMMITTED {
		t.Fatalf("expected COMMITTED cancelled trial, got %v", compResp.Status)
	}

	// 5. Verify cancellation in DB
	var cancelledCount int
	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT cancelled_count FROM %s.experiments WHERE experiment_id=$1`, schema), matResult.ExperimentID).Scan(&cancelledCount)
	if err != nil || cancelledCount < 1 {
		t.Fatalf("expected cancelled_count >= 1, got %d (err=%v)", cancelledCount, err)
	}
}
