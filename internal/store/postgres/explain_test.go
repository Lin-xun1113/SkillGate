package postgres

import (
	"strings"
	"testing"
)

func TestM2ClaimAndExpiryIndexesAreInstalled(t *testing.T) {
	store, ctx := integrationStore(t)
	rows, err := store.pool.Query(ctx, `
SELECT indexname FROM pg_indexes
WHERE schemaname=current_schema() AND tablename='trial_attempts'
  AND indexname IN ('trial_attempts_claim_idx','trial_attempts_expiry_idx')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found["trial_attempts_claim_idx"] || !found["trial_attempts_expiry_idx"] {
		t.Fatalf("M2 partial indexes missing: %#v", found)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	rows, err = tx.Query(ctx, `EXPLAIN (COSTS OFF) SELECT trial_id FROM trial_attempts WHERE status='PENDING' AND not_before <= clock_timestamp() ORDER BY priority DESC, not_before, created_at, trial_id LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "trial_attempts_claim_idx") {
		t.Fatalf("claim plan did not use target index: %s", strings.Join(plan, "\n"))
	}
}
