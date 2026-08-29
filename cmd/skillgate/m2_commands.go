package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	"github.com/Lin-xun1113/SkillGate/internal/secrets"
	storepg "github.com/Lin-xun1113/SkillGate/internal/store/postgres"
)

func databaseCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	if len(args) < 1 {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "db", "db 子命令不能为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), durationFlag(args, "--timeout", 30*time.Second))
	defer cancel()
	databaseURL, err := databaseURL(args)
	if err != nil {
		return returnSchedulerError(jsonOutput, "database", err)
	}
	db, err := storepg.OpenSQL(databaseURL)
	if err != nil {
		return returnSchedulerError(jsonOutput, "database", err)
	}
	defer db.Close()
	switch args[0] {
	case "migrate":
		if err := storepg.Migrate(ctx, db); err != nil {
			return returnWithError(jsonOutput, "MIGRATION_FAILED", "database", safeError(err))
		}
		current, target, err := storepg.MigrationVersions(ctx, db)
		if err != nil {
			return returnWithError(jsonOutput, "MIGRATION_FAILED", "database", safeError(err))
		}
		return printOperationSuccess(jsonOutput, "db.migrate", map[string]any{"current_version": current, "target_version": target})
	case "status", "version":
		current, target, err := storepg.MigrationVersions(ctx, db)
		if err != nil {
			return returnWithError(jsonOutput, "MIGRATION_FAILED", "database", safeError(err))
		}
		return printOperationSuccess(jsonOutput, "db."+args[0], map[string]any{"current_version": current, "target_version": target, "pending": current != target})
	default:
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", args[0], "未知 db 子命令")
	}
}

func experimentCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	if len(args) < 1 {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "experiment", "experiment 子命令不能为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), durationFlag(args, "--timeout", 30*time.Second))
	defer cancel()
	store, code := openM2Store(ctx, args, jsonOutput)
	if code != 0 {
		return code
	}
	defer store.Close()
	switch args[0] {
	case "materialize":
		manifestPath := positional(args[1:])
		if manifestPath == "" {
			return returnWithError(jsonOutput, "INVALID_ARGUMENT", "experiment.materialize", "Manifest 路径不能为空")
		}
		root, err := os.Getwd()
		if err != nil {
			return returnWithError(jsonOutput, "IO_ERROR", "", err.Error())
		}
		compiled, diagnostics := manifest.Compile(manifestPath, root)
		if len(diagnostics) > 0 {
			return returnDiagnostics(jsonOutput, diagnostics)
		}
		options := scheduler.MaterializeOptions{
			Priority: intFlag(args, "--priority", 0), MaxAttempts: compiled.Runtime.MaxAttempts,
			Timeout: time.Duration(compiled.Runtime.AttemptTimeoutSeconds) * time.Second, BudgetTimeout: durationFlag(args, "--budget-timeout", 10*time.Minute),
			BackoffBase: durationFlag(args, "--backoff-base", time.Second), BackoffCap: durationFlag(args, "--backoff-cap", 30*time.Second),
		}
		if options.MaxAttempts < 1 {
			options.MaxAttempts = 2
		}
		if options.Timeout <= 0 {
			options.Timeout = 60 * time.Second
		}
		for _, category := range compiled.Runtime.RetryableCategories {
			options.Retryable = append(options.Retryable, retry.Category(category))
		}
		if len(options.Retryable) == 0 {
			options.Retryable = []retry.Category{retry.ProviderTransient, retry.DependencyTransient, retry.WorkerLost, retry.LeaseTimeout}
		}
		if value := flagValue(args, "--max-attempts", ""); value != "" {
			options.MaxAttempts = intFlag(args, "--max-attempts", options.MaxAttempts)
		}
		if value := flagValue(args, "--trial-timeout", ""); value != "" {
			options.Timeout = durationFlag(args, "--trial-timeout", options.Timeout)
		}
		result, err := store.Materialize(ctx, compiled, options)
		if err != nil {
			return returnSchedulerError(jsonOutput, manifestPath, err)
		}
		return printOperationSuccess(jsonOutput, "experiment.materialize", result)
	case "cancel":
		experimentID := flagValue(args, "--experiment-id", "")
		result, err := store.CancelExperiment(ctx, experimentID, flagValue(args, "--actor", "skillgate-cli"), flagValue(args, "--reason", "operator request"))
		if err != nil {
			return returnSchedulerError(jsonOutput, experimentID, err)
		}
		return printOperationSuccess(jsonOutput, "experiment.cancel", result)
	default:
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", args[0], "未知 experiment 子命令")
	}
}

func trialCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	if len(args) < 1 {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "trial", "trial 子命令不能为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), durationFlag(args, "--timeout", 30*time.Second))
	defer cancel()
	store, code := openM2Store(ctx, args, jsonOutput)
	if code != 0 {
		return code
	}
	defer store.Close()
	switch args[0] {
	case "claim":
		result, err := store.Claim(ctx, flagValue(args, "--worker-id", ""), durationFlag(args, "--lease-duration", 30*time.Second))
		if err != nil {
			return returnSchedulerError(jsonOutput, "trial.claim", err)
		}
		return printOperationSuccess(jsonOutput, "trial.claim", result)
	case "start", "heartbeat":
		heartbeat, err := heartbeatFromFlags(args)
		if err != nil {
			return returnSchedulerError(jsonOutput, "trial."+args[0], err)
		}
		if args[0] == "start" {
			err = store.Start(ctx, heartbeat)
		} else {
			err = store.Heartbeat(ctx, heartbeat, durationFlag(args, "--lease-duration", 30*time.Second))
		}
		if err != nil {
			return returnSchedulerError(jsonOutput, heartbeat.TrialID, err)
		}
		return printOperationSuccess(jsonOutput, "trial."+args[0], map[string]any{"trial_id": heartbeat.TrialID, "accepted": true})
	case "complete":
		completion, err := completionFromFlags(args)
		if err != nil {
			return returnSchedulerError(jsonOutput, "trial.complete", err)
		}
		result, err := store.Complete(ctx, completion)
		if err != nil {
			return returnSchedulerError(jsonOutput, completion.TrialID, err)
		}
		return printOperationSuccess(jsonOutput, "trial.complete", result)
	default:
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", args[0], "未知 trial 子命令")
	}
}

func schedulerCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	if len(args) < 1 || args[0] != "sweep" {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "scheduler", "仅支持 scheduler sweep")
	}
	ctx, cancel := context.WithTimeout(context.Background(), durationFlag(args, "--timeout", 30*time.Second))
	defer cancel()
	store, code := openM2Store(ctx, args, jsonOutput)
	if code != 0 {
		return code
	}
	defer store.Close()
	result, err := store.SweepExpired(ctx, intFlag(args, "--limit", 100))
	if err != nil {
		return returnSchedulerError(jsonOutput, "scheduler.sweep", err)
	}
	return printOperationSuccess(jsonOutput, "scheduler.sweep", result)
}

func heartbeatFromFlags(args []string) (scheduler.Heartbeat, error) {
	token, err := readSecretFile(flagValue(args, "--lease-token-file", ""))
	if err != nil {
		return scheduler.Heartbeat{}, err
	}
	return scheduler.Heartbeat{
		TrialID: flagValue(args, "--trial-id", ""), WorkerID: flagValue(args, "--worker-id", ""), LeaseToken: token,
		LeaseGeneration: int64(intFlag(args, "--lease-generation", 0)), EventSequence: int64(intFlag(args, "--event-sequence", 0)), Phase: flagValue(args, "--phase", ""),
	}, nil
}

func completionFromFlags(args []string) (scheduler.Completion, error) {
	token, err := readSecretFile(flagValue(args, "--lease-token-file", ""))
	if err != nil {
		return scheduler.Completion{}, err
	}
	path := flagValue(args, "--result-manifest", "")
	raw, err := os.ReadFile(path)
	if err != nil {
		return scheduler.Completion{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法读取 result manifest", Cause: err}
	}
	hash, err := scheduler.ResultManifestHash(raw)
	if err != nil {
		return scheduler.Completion{}, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: err.Error(), Cause: err}
	}
	return scheduler.Completion{
		LogicalTrialID: flagValue(args, "--logical-trial-id", ""), TrialID: flagValue(args, "--trial-id", ""),
		WorkerID: flagValue(args, "--worker-id", ""), LeaseToken: token, LeaseGeneration: int64(intFlag(args, "--lease-generation", 0)),
		RequestHash: flagValue(args, "--request-hash", ""), IdempotencyKey: flagValue(args, "--idempotency-key", ""),
		ManifestHash: hash, Manifest: raw, Outcome: scheduler.Outcome(strings.ToUpper(flagValue(args, "--outcome", ""))),
		Category: retry.Category(strings.ToUpper(flagValue(args, "--category", ""))), EventSequence: int64(intFlag(args, "--event-sequence", 0)),
	}, nil
}

func openM2Store(ctx context.Context, args []string, jsonOutput bool) (*storepg.Store, int) {
	databaseURL, err := databaseURL(args)
	if err != nil {
		return nil, returnSchedulerError(jsonOutput, "database", err)
	}
	store, err := storepg.Open(ctx, databaseURL)
	if err != nil {
		return nil, returnSchedulerError(jsonOutput, "database", err)
	}
	return store, 0
}

func databaseURL(args []string) (string, error) {
	if hasFlag(args, "--database-url-file") {
		path := flagValue(args, "--database-url-file", "")
		if strings.TrimSpace(path) == "" {
			return "", &scheduler.Error{Code: scheduler.CodeSecretEmpty, Message: "--database-url-file 不能为空"}
		}
		return readDatabaseURLFile(path)
	}
	// Keep the legacy flag for source compatibility, but reject a password in
	// the argument itself: command lines are visible through shell history and
	// container process inspection. Operators should use *_FILE or an env var.
	if value := flagValue(args, "--database-url", ""); value != "" {
		if err := rejectInlineDatabaseCredential(value); err != nil {
			return "", err
		}
		return value, nil
	}
	for _, name := range []string{"SKILLGATE_DATABASE_URL", "DATABASE_URL"} {
		if secrets.Configured(name) {
			value, err := secrets.Lookup(name)
			if err != nil {
				return "", secretSchedulerError(err)
			}
			return value, nil
		}
	}
	return "", &scheduler.Error{Code: scheduler.CodeSecretMissing, Message: "必须设置 SKILLGATE_DATABASE_URL_FILE、SKILLGATE_DATABASE_URL 或 DATABASE_URL_FILE"}
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func readDatabaseURLFile(path string) (string, error) {
	value, err := secrets.LookupWith("SKILLGATE_DATABASE_URL", func(name string) (string, bool) {
		if name == "SKILLGATE_DATABASE_URL_FILE" {
			return path, true
		}
		return "", false
	}, nil)
	if err != nil {
		return "", secretSchedulerError(err)
	}
	return value, nil
}

func secretSchedulerError(err error) error {
	var secretErr *secrets.Error
	if errors.As(err, &secretErr) {
		return &scheduler.Error{Code: scheduler.ErrorCode(secretErr.Code), Message: secretErr.Error(), Cause: err}
	}
	return &scheduler.Error{Code: scheduler.CodeSecretUnreadable, Message: "无法读取数据库 Secret", Cause: err}
}

func rejectInlineDatabaseCredential(value string) error {
	parsed, err := url.Parse(value)
	if err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "数据库密码不得通过 --database-url 命令行传递；请使用 --database-url-file 或 SKILLGATE_DATABASE_URL_FILE"}
		}
	}
	// Also reject libpq-style DSNs if a caller passes one despite the URL name.
	lower := strings.ToLower(value)
	for _, marker := range []string{"password=", "passfile=", "sslkey="} {
		if strings.Contains(lower, marker) {
			return &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "数据库 Secret 不得通过命令行传递；请使用 --database-url-file 或 *_FILE"}
		}
	}
	return nil
}

func readSecretFile(path string) (string, error) {
	if path == "" {
		return "", &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "lease token 必须通过 --lease-token-file 提供"}
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "无法读取 lease token 文件", Cause: err}
	}
	token := strings.TrimSpace(string(value))
	if token == "" {
		return "", &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "lease token 文件为空"}
	}
	return token, nil
}

func printOperationSuccess(jsonOutput bool, operation string, result any) int {
	if jsonOutput {
		data, _ := json.Marshal(map[string]any{"version": cliVersion, "ok": true, "operation": operation, "data": result, "diagnostics": []any{}})
		fmt.Println(string(data))
		return 0
	}
	encoded, _ := json.Marshal(result)
	fmt.Printf("operation=%s ok=true data=%s\n", operation, encoded)
	return 0
}

func returnSchedulerError(jsonOutput bool, path string, err error) int {
	code := scheduler.CodeOf(err)
	if code == "" {
		code = scheduler.CodeDatabaseUnavailable
	}
	return returnWithError(jsonOutput, string(code), path, safeError(err))
}

func safeError(err error) string {
	if code := scheduler.CodeOf(err); code != "" {
		return err.Error()
	}
	return "数据库操作失败；详细原因已保留在本机诊断中。"
}

func positional(args []string) string {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			if args[i] != "--json" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
			}
			continue
		}
		return args[i]
	}
	return ""
}

func intFlag(args []string, name string, fallback int) int {
	value := flagValue(args, name, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationFlag(args []string, name string, fallback time.Duration) time.Duration {
	value := flagValue(args, name, "")
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
