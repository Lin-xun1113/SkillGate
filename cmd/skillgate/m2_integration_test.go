package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestM2CLIRealPostgresLifecycle(t *testing.T) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("需要 TEST_DATABASE_URL 指向真实 PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("skillgate_cli_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = pool.Exec(cleanup, `DROP SCHEMA IF EXISTS `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		pool.Close()
	}()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	databaseURL := parsed.String()

	root := filepath.Join("..", "..")
	binary := filepath.Join(t.TempDir(), "skillgate")
	build := exec.Command("go", "build", "-o", binary, "./cmd/skillgate")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	run := func(args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "SKILLGATE_DATABASE_URL="+databaseURL)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("CLI %v: %v\n%s", args, err, output)
		}
		var document map[string]any
		if err := json.Unmarshal(output, &document); err != nil {
			t.Fatalf("CLI %v 输出不是 JSON: %v\n%s", args, err, output)
		}
		if document["version"] != "skillgate.cli.v1" || document["ok"] != true {
			t.Fatalf("CLI %v envelope=%#v", args, document)
		}
		return document
	}

	run("db", "migrate", "--json")
	materialized := run("experiment", "materialize", "experiments/csv-analysis-v1-demo.yaml", "--json")
	if materialized["operation"] != "experiment.materialize" {
		t.Fatalf("materialize operation=%#v", materialized)
	}
	claimed := run("trial", "claim", "--worker-id", "cli-fixture", "--lease-duration", "10s", "--json")
	claimData, ok := claimed["data"].(map[string]any)
	if !ok {
		t.Fatalf("claim data=%#v", claimed)
	}
	token, _ := claimData["lease_token"].(string)
	trialID, _ := claimData["trial_id"].(string)
	logicalTrialID, _ := claimData["logical_trial_id"].(string)
	generation := int(claimData["lease_generation"].(float64))
	if token == "" || trialID == "" || logicalTrialID == "" || generation < 1 {
		t.Fatalf("claim identity 不完整: %#v", claimData)
	}
	tokenFile := filepath.Join(t.TempDir(), "lease-token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	common := []string{"--trial-id", trialID, "--worker-id", "cli-fixture", "--lease-token-file", tokenFile, "--lease-generation", "1", "--event-sequence", "1", "--json"}
	run(append([]string{"trial", "start"}, common...)...)
	resultFile := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(resultFile, []byte(`{"version":"skillgate.result.v1","fixture":"cli"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	completeArgs := []string{"trial", "complete", "--logical-trial-id", logicalTrialID, "--result-manifest", resultFile, "--outcome", "SUCCEEDED"}
	completeArgs = append(completeArgs, common...)
	completed := run(completeArgs...)
	if completed["operation"] != "trial.complete" {
		t.Fatalf("complete operation=%#v", completed)
	}
}
