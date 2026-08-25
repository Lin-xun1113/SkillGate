package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/experiment"
	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("需要 TEST_DATABASE_URL 指向真实 PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	basePool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("skillgate_store_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := basePool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		basePool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = basePool.Exec(cleanup, `DROP SCHEMA IF EXISTS `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		basePool.Close()
	})
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	databaseURL := parsed.String()
	db, err := OpenSQL(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("重复 migration 必须幂等: %v", err)
	}
	db.Close()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	store.WithJitter(func(time.Duration) time.Duration { return 0 })
	t.Cleanup(store.Close)
	return store, ctx
}

func samplePlan(t *testing.T, pairs int) *manifest.CompiledExperiment {
	t.Helper()
	plan := &manifest.CompiledExperiment{
		ManifestHash: "sha256:manifest", SuiteHash: "sha256:suite",
		Pairing: map[string]string{"treatment": "skill_version", "baseline_arm": "without_skill", "candidate_arm": "with_skill"},
		Pairs:   make([]experiment.PairPlan, 0, pairs),
	}
	for i := 0; i < pairs; i++ {
		pairID := fmt.Sprintf("sha256:pair-%02d", i)
		without, err := identity.TrialID(pairID, "without_skill", 1)
		if err != nil {
			t.Fatal(err)
		}
		with, err := identity.TrialID(pairID, "with_skill", 1)
		if err != nil {
			t.Fatal(err)
		}
		plan.Pairs = append(plan.Pairs, experiment.PairPlan{
			PairID: pairID, CaseID: fmt.Sprintf("case-%02d", i), Repetition: 1,
			Trials: []experiment.Trial{{TrialID: without, Arm: "without_skill", Attempt: 1}, {TrialID: with, Arm: "with_skill", Attempt: 1}},
		})
	}
	plan.PairCount = pairs
	plan.TrialCount = pairs * 2
	return plan
}

func sampleOptions() scheduler.MaterializeOptions {
	return scheduler.MaterializeOptions{
		Priority: 1, MaxAttempts: 2, Timeout: 2 * time.Second, BudgetTimeout: time.Minute,
		BackoffBase: time.Millisecond, BackoffCap: 10 * time.Millisecond,
		Retryable: []retry.Category{retry.WorkerLost, retry.ProviderTransient, retry.LeaseTimeout},
	}
}

func materialize(t *testing.T, store *Store, ctx context.Context, pairs int) MaterializeResult {
	t.Helper()
	result, err := store.Materialize(ctx, samplePlan(t, pairs), sampleOptions())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func resultManifest(t *testing.T, marker string) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": "skillgate.result.v1", "marker": marker})
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw, hash
}

func completionFor(claim scheduler.Claim, manifest []byte, hash string, outcome scheduler.Outcome, category retry.Category) scheduler.Completion {
	requestHash, _ := scheduler.TrialRequestHash(claim.ExperimentID, claim.LogicalTrialID, claim.TrialID, claim.PairID, claim.Arm, claim.Attempt)
	idempotencyKey, _ := scheduler.ResultIdempotencyKey(claim.TrialID, hash)
	return scheduler.Completion{
		LogicalTrialID: claim.LogicalTrialID, TrialID: claim.TrialID, WorkerID: claim.WorkerID,
		LeaseToken: claim.LeaseToken, LeaseGeneration: claim.LeaseGeneration,
		RequestHash: requestHash, IdempotencyKey: idempotencyKey,
		ManifestHash: hash, Manifest: manifest, Outcome: outcome, Category: category, EventSequence: 1,
	}
}

func TestMigrationAndMaterializationAreIdempotent(t *testing.T) {
	store, ctx := integrationStore(t)
	first := materialize(t, store, ctx, 2)
	second, err := store.Materialize(ctx, samplePlan(t, 2), sampleOptions())
	if err != nil {
		t.Fatal(err)
	}
	if first.ExperimentID != second.ExperimentID || !second.Idempotent || second.LogicalTrials != 4 {
		t.Fatalf("物化幂等结果错误: %#v %#v", first, second)
	}
	var logical, attempts int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM logical_trials`).Scan(&logical); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM trial_attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if logical != 4 || attempts != 4 {
		t.Fatalf("重复物化产生额外行: logical=%d attempts=%d", logical, attempts)
	}
}

func TestConcurrentMaterializationIsIdempotent(t *testing.T) {
	store, ctx := integrationStore(t)
	plan := samplePlan(t, 2)
	results := make(chan MaterializeResult, 8)
	errs := make(chan error, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := store.Materialize(ctx, plan, sampleOptions())
			results <- result
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var experimentID string
	for result := range results {
		if experimentID == "" {
			experimentID = result.ExperimentID
		}
		if result.ExperimentID != experimentID {
			t.Fatalf("并发物化产生不同 experiment: %#v", result)
		}
	}
	var experiments, trials, attempts int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM experiments`).Scan(&experiments); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM logical_trials`).Scan(&trials); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM trial_attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if experiments != 1 || trials != 4 || attempts != 4 {
		t.Fatalf("并发物化重复: experiments=%d trials=%d attempts=%d", experiments, trials, attempts)
	}
}

func TestConcurrentClaimNeverDuplicatesAttempt(t *testing.T) {
	store, ctx := integrationStore(t)
	materialize(t, store, ctx, 20)
	claims := make(chan scheduler.Claim, 40)
	errs := make(chan error, 40)
	var group sync.WaitGroup
	for i := 0; i < 40; i++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			claim, err := store.Claim(ctx, fmt.Sprintf("worker-%02d", worker), time.Second)
			if err != nil {
				errs <- err
				return
			}
			claims <- claim
		}(i)
	}
	group.Wait()
	close(claims)
	close(errs)
	seen := map[string]bool{}
	for claim := range claims {
		if seen[claim.TrialID] {
			t.Fatalf("attempt 被重复领取: %s", claim.TrialID)
		}
		seen[claim.TrialID] = true
		if claim.LeaseToken == "" || claim.LeaseGeneration != 1 {
			t.Fatalf("claim 缺少 token/fence: %#v", claim)
		}
	}
	for err := range errs {
		if scheduler.CodeOf(err) != scheduler.CodeNotClaimable {
			t.Fatalf("并发 claim 失败: %v", err)
		}
	}
	if len(seen) != 40 {
		t.Fatalf("expected 40 claims, got %d", len(seen))
	}
}

func TestHeartbeatRejectsWrongOwnerTokenAndExpiredLease(t *testing.T) {
	store, ctx := integrationStore(t)
	materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "worker-1", 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	base := scheduler.Heartbeat{TrialID: claim.TrialID, WorkerID: claim.WorkerID, LeaseToken: claim.LeaseToken, LeaseGeneration: claim.LeaseGeneration, EventSequence: 1, Phase: "invoke"}
	wrongOwner := base
	wrongOwner.WorkerID = "worker-2"
	if code := scheduler.CodeOf(store.Start(ctx, wrongOwner)); code != scheduler.CodeOwnerMismatch {
		t.Fatalf("wrong owner code=%s", code)
	}
	wrongToken := base
	wrongToken.LeaseToken = "wrong"
	if code := scheduler.CodeOf(store.Start(ctx, wrongToken)); code != scheduler.CodeLeaseMismatch {
		t.Fatalf("wrong token code=%s", code)
	}
	if err := store.Start(ctx, base); err != nil {
		t.Fatal(err)
	}
	if err := store.Heartbeat(ctx, base, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE trial_attempts SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE trial_id=$1`, claim.TrialID); err != nil {
		t.Fatal(err)
	}
	if code := scheduler.CodeOf(store.Heartbeat(ctx, base, time.Second)); code != scheduler.CodeLeaseExpired {
		t.Fatalf("expired heartbeat code=%s", code)
	}
}

func TestCompletionReplayRequiresOriginalLeaseAuthority(t *testing.T) {
	store, ctx := integrationStore(t)
	materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "authorized-worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, hash := resultManifest(t, "authorized")
	completion := completionFor(claim, manifestJSON, hash, scheduler.OutcomeSucceeded, "")
	if _, err := store.Complete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	unauthorized := completion
	unauthorized.WorkerID = "attacker"
	if code := scheduler.CodeOf(func() error { _, err := store.Complete(ctx, unauthorized); return err }()); code != scheduler.CodeOwnerMismatch {
		t.Fatalf("unauthorized replay code=%s", code)
	}
}

func TestCompletionThreeTimesCountsOnceAndConflictDoesNotOverwrite(t *testing.T) {
	store, ctx := integrationStore(t)
	created := materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, hash := resultManifest(t, "same")
	completion := completionFor(claim, manifestJSON, hash, scheduler.OutcomeSucceeded, "")
	for i := 0; i < 3; i++ {
		result, err := store.Complete(ctx, completion)
		if err != nil {
			t.Fatal(err)
		}
		if (i == 0 && result.Idempotent) || (i > 0 && !result.Idempotent) {
			t.Fatalf("idempotent flag at %d: %#v", i, result)
		}
	}
	otherJSON, otherHash := resultManifest(t, "different")
	conflict := completionFor(claim, otherJSON, otherHash, scheduler.OutcomeSucceeded, "")
	if code := scheduler.CodeOf(func() error { _, err := store.Complete(ctx, conflict); return err }()); code != scheduler.CodeResultConflict {
		t.Fatalf("conflict code=%s", code)
	}
	var terminal, succeeded, results int
	if err := store.pool.QueryRow(ctx, `SELECT terminal_count,succeeded_count FROM experiments WHERE experiment_id=$1`, created.ExperimentID).Scan(&terminal, &succeeded); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM trial_results WHERE logical_trial_id=$1`, claim.LogicalTrialID).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if terminal != 1 || succeeded != 1 || results != 1 {
		t.Fatalf("重复计数: terminal=%d succeeded=%d results=%d", terminal, succeeded, results)
	}
}

func TestExpiredLeaseRetriesAndLateCompletionIsRejected(t *testing.T) {
	store, ctx := integrationStore(t)
	materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "old-worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE trial_attempts SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE trial_id=$1`, claim.TrialID); err != nil {
		t.Fatal(err)
	}
	swept, err := store.SweepExpired(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if swept.Processed != 1 || swept.Retried != 1 {
		t.Fatalf("unexpected sweep: %#v", swept)
	}
	manifestJSON, hash := resultManifest(t, "late")
	late := completionFor(claim, manifestJSON, hash, scheduler.OutcomeSucceeded, "")
	if code := scheduler.CodeOf(func() error { _, err := store.Complete(ctx, late); return err }()); code != scheduler.CodeLeaseExpired {
		t.Fatalf("late completion code=%s", code)
	}
	var newClaim scheduler.Claim
	for i := 0; i < 2; i++ {
		candidate, claimErr := store.Claim(ctx, fmt.Sprintf("new-worker-%d", i), time.Second)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if candidate.LogicalTrialID == claim.LogicalTrialID {
			newClaim = candidate
			break
		}
	}
	if newClaim.Attempt != 2 || newClaim.LeaseGeneration != 2 || newClaim.TrialID == claim.TrialID {
		t.Fatalf("retry identity/fence 错误: old=%#v new=%#v", claim, newClaim)
	}
}

func TestPendingTrialsConvergeWhenBudgetExpires(t *testing.T) {
	store, ctx := integrationStore(t)
	created := materialize(t, store, ctx, 1)
	if _, err := store.pool.Exec(ctx, `UPDATE experiments SET budget_deadline_at=clock_timestamp()-interval '1 second' WHERE experiment_id=$1`, created.ExperimentID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(ctx, "worker", time.Second); scheduler.CodeOf(err) != scheduler.CodeNotClaimable {
		t.Fatalf("预算耗尽后仍可 claim: %v", err)
	}
	result, err := store.SweepExpired(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Terminal != 2 || result.Retried != 0 {
		t.Fatalf("pending budget sweep=%#v", result)
	}
	var status scheduler.ExperimentStatus
	if err := store.pool.QueryRow(ctx, `SELECT status FROM experiments WHERE experiment_id=$1`, created.ExperimentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != scheduler.ExperimentGrading {
		t.Fatalf("预算耗尽未收敛: %s", status)
	}
}

func TestExpiredLeaseStopsRetryWhenBudgetIsExhausted(t *testing.T) {
	store, ctx := integrationStore(t)
	materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "budget-worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE trial_attempts SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE trial_id=$1`, claim.TrialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE experiments SET budget_deadline_at=clock_timestamp()-interval '1 second' WHERE experiment_id=$1`, claim.ExperimentID); err != nil {
		t.Fatal(err)
	}
	result, err := store.SweepExpired(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Terminal != 2 || result.Retried != 0 {
		t.Fatalf("预算耗尽未终结全部工作或仍创建 retry: %#v", result)
	}
	var status scheduler.TrialStatus
	var attempts int
	if err := store.pool.QueryRow(ctx, `SELECT status FROM logical_trials WHERE logical_trial_id=$1`, claim.LogicalTrialID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM trial_attempts WHERE logical_trial_id=$1`, claim.LogicalTrialID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if status != scheduler.TrialFailed || attempts != 1 {
		t.Fatalf("budget terminal status=%s attempts=%d", status, attempts)
	}
}

func TestCommitCanBeRetriedAfterResponseLoss(t *testing.T) {
	store, ctx := integrationStore(t)
	materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, hash := resultManifest(t, "response-lost")
	completion := completionFor(claim, manifestJSON, hash, scheduler.OutcomeSucceeded, "")
	if _, err := store.Complete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	result, err := store.Complete(ctx, completion)
	if err != nil || !result.Idempotent {
		t.Fatalf("commit 重发失败: %#v %v", result, err)
	}
}

func TestCancelAndCompletionRaceDoesNotDeadlockOrDoubleCount(t *testing.T) {
	store, parent := integrationStore(t)
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	created := materialize(t, store, ctx, 1)
	claim, err := store.Claim(ctx, "race-worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, hash := resultManifest(t, "race")
	completion := completionFor(claim, manifestJSON, hash, scheduler.OutcomeSucceeded, "")
	errs := make(chan error, 2)
	go func() {
		_, err := store.Complete(ctx, completion)
		if code := scheduler.CodeOf(err); err != nil && code != scheduler.CodeExperimentCancelRequested && code != scheduler.CodeLeaseExpired {
			errs <- err
			return
		}
		errs <- nil
	}()
	go func() {
		_, err := store.CancelExperiment(ctx, created.ExperimentID, "race-test", "cancel/complete race")
		errs <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("cancel/complete 竞态超时: %v", ctx.Err())
	}
	if _, err := store.pool.Exec(ctx, `UPDATE trial_attempts SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE trial_id=$1 AND status IN ('LEASED','RUNNING')`, claim.TrialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SweepExpired(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var total, terminal, succeeded, cancelledCount int
	if err := store.pool.QueryRow(ctx, `SELECT total_logical_trials,terminal_count,succeeded_count,cancelled_count FROM experiments WHERE experiment_id=$1`, created.ExperimentID).Scan(&total, &terminal, &succeeded, &cancelledCount); err != nil {
		t.Fatal(err)
	}
	if terminal != total || succeeded+cancelledCount != total {
		t.Fatalf("race counter 不一致: total=%d terminal=%d succeeded=%d cancelled=%d", total, terminal, succeeded, cancelledCount)
	}
}

func TestCancellationStopsClaimsAndConvergesAfterRunningLeaseExpires(t *testing.T) {
	store, ctx := integrationStore(t)
	created := materialize(t, store, ctx, 2)
	claim, err := store.Claim(ctx, "worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.CancelExperiment(ctx, created.ExperimentID, "test", "fault injection")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != scheduler.ExperimentCancelRequested || cancelled.Cancelled != 3 {
		t.Fatalf("cancel request 错误: %#v", cancelled)
	}
	if _, err := store.Claim(ctx, "other", time.Second); scheduler.CodeOf(err) != scheduler.CodeNotClaimable {
		t.Fatalf("cancel 后仍可 claim: %v", err)
	}
	manifestJSON, hash := resultManifest(t, "too-late-success")
	success := completionFor(claim, manifestJSON, hash, scheduler.OutcomeSucceeded, "")
	if _, err := store.Complete(ctx, success); scheduler.CodeOf(err) != scheduler.CodeExperimentCancelRequested {
		t.Fatalf("cancel 后 success code=%s", scheduler.CodeOf(err))
	}
	if _, err := store.pool.Exec(ctx, `UPDATE trial_attempts SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE trial_id=$1`, claim.TrialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SweepExpired(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var status scheduler.ExperimentStatus
	var terminal, cancelledCount int
	if err := store.pool.QueryRow(ctx, `SELECT status,terminal_count,cancelled_count FROM experiments WHERE experiment_id=$1`, created.ExperimentID).Scan(&status, &terminal, &cancelledCount); err != nil {
		t.Fatal(err)
	}
	if status != scheduler.ExperimentCancelled || terminal != 4 || cancelledCount != 4 {
		t.Fatalf("cancel 未收敛: status=%s terminal=%d cancelled=%d", status, terminal, cancelledCount)
	}
	again, err := store.CancelExperiment(ctx, created.ExperimentID, "test", "again")
	if err != nil || !again.Idempotent || again.Status != scheduler.ExperimentCancelled {
		t.Fatalf("重复 cancel 不幂等: %#v %v", again, err)
	}
}
