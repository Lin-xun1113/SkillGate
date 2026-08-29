package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// TrialRunner orchestrates trial execution: projection → sandbox → worker.
type TrialRunner struct {
	projector       *Projector
	casDir          string
	workspaceBase   string
	workerImage     string
	timeoutSeconds  int
	memoryLimitMB   int64
	cpuQuota        int64
	networkDisabled bool
}

// TrialRunnerConfig holds runner configuration.
type TrialRunnerConfig struct {
	ProjectRoot     string
	CASDir          string
	WorkspaceBase   string
	WorkerImage     string
	TimeoutSeconds  int
	MemoryLimitMB   int64
	CPUQuota        int64
	NetworkDisabled bool
}

// NewTrialRunner creates a new trial runner.
func NewTrialRunner(config TrialRunnerConfig) *TrialRunner {
	return &TrialRunner{
		projector:       NewProjector(config.ProjectRoot),
		casDir:          config.CASDir,
		workspaceBase:   config.WorkspaceBase,
		workerImage:     config.WorkerImage,
		timeoutSeconds:  config.TimeoutSeconds,
		memoryLimitMB:   config.MemoryLimitMB,
		cpuQuota:        config.CPUQuota,
		networkDisabled: config.NetworkDisabled,
	}
}

// ExecuteTrial runs a complete trial: project execution, launch sandbox, collect result.
func (r *TrialRunner) ExecuteTrial(ctx context.Context, claim scheduler.Claim, manifestPath string) (TrialExecutionResult, error) {
	startTime := time.Now()

	// Step 1: Build base payload (M3 frozen request hash)
	basePayload, requestHash, err := BuildTrialRequestPayload(claim)
	if err != nil {
		return TrialExecutionResult{}, fmt.Errorf("failed to build payload: %w", err)
	}

	// Step 2: Project execution from manifest
	executionSpec, executionHash, err := r.projector.ProjectForClaim(manifestPath, claim.PairID, claim.Arm)
	if err != nil {
		return TrialExecutionResult{}, fmt.Errorf("failed to project execution: %w", err)
	}

	// Step 3: Inject execution into payload
	fullPayload, _, _, err := ProjectExecution(
		basePayload,
		requestHash,
		claim.ManifestHash,
		executionSpec.CaseID,
		claim.Arm, // arm parameter
		executionSpec.CaseInput,
		executionSpec.SkillHash,
		executionSpec.Model,
		executionSpec.ToolPolicy,
		executionSpec.Environment,
	)
	if err != nil {
		return TrialExecutionResult{}, fmt.Errorf("failed to inject execution: %w", err)
	}

	// Step 4: Prepare workspace
	workspaceDir, err := PrepareWorkspace(r.workspaceBase, claim.TrialID)
	if err != nil {
		return TrialExecutionResult{}, fmt.Errorf("failed to prepare workspace: %w", err)
	}

	// Write trial request to workspace
	requestPath := filepath.Join(workspaceDir, "request.json")
	if err := os.WriteFile(requestPath, []byte(fullPayload), 0644); err != nil {
		return TrialExecutionResult{}, fmt.Errorf("failed to write request: %w", err)
	}

	// Step 5: Launch sandbox and execute
	sandboxConfig := SandboxConfig{
		Image:           r.workerImage,
		CASRoot:         r.casDir,
		ArtifactsRoot:   workspaceDir,
		Env:             []string{},
		MemoryLimitMB:   r.memoryLimitMB,
		CPUQuota:        r.cpuQuota,
		NetworkDisabled: r.networkDisabled,
		ReadOnlyRoot:    true,
		User:            "1000:1000",
	}

	exitCode, logs, err := RunTrial(ctx, sandboxConfig)
	if err != nil {
		return TrialExecutionResult{
			TrialID:       claim.TrialID,
			RequestHash:   requestHash,
			ExecutionHash: executionHash,
			Outcome:       "error",
			Category:      "sandbox_error",
			Message:       err.Error(),
			Logs:          logs,
			DurationMs:    time.Since(startTime).Milliseconds(),
		}, nil // Return result, not error - we have structured failure
	}

	// Step 6: Parse result
	resultPath := filepath.Join(workspaceDir, "result.json")
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		return TrialExecutionResult{
			TrialID:       claim.TrialID,
			RequestHash:   requestHash,
			ExecutionHash: executionHash,
			Outcome:       "error",
			Category:      "result_missing",
			Message:       fmt.Sprintf("Failed to read result: %v", err),
			Logs:          logs,
			DurationMs:    time.Since(startTime).Milliseconds(),
		}, nil
	}

	var workerResult struct {
		TrialID  string           `json:"trial_id"`
		Outcome  string           `json:"outcome"`
		Category string           `json:"category,omitempty"`
		Message  string           `json:"message,omitempty"`
		Events   []map[string]any `json:"events"`
		// The local worker emits the protocol ArtifactManifest list while older
		// sandbox workers emitted a name -> hash object. Decode both forms below
		// so upgrading the worker cannot turn a valid result into result_invalid.
		Artifacts json.RawMessage `json:"artifacts"`
		Trace     map[string]any  `json:"trace,omitempty"`
	}

	if err := json.Unmarshal(resultData, &workerResult); err != nil {
		return TrialExecutionResult{
			TrialID:       claim.TrialID,
			RequestHash:   requestHash,
			ExecutionHash: executionHash,
			Outcome:       "error",
			Category:      "result_invalid",
			Message:       fmt.Sprintf("Failed to parse result: %v", err),
			Logs:          logs,
			DurationMs:    time.Since(startTime).Milliseconds(),
		}, nil
	}

	artifacts, err := decodeWorkerArtifacts(workerResult.Artifacts)
	if err != nil {
		return TrialExecutionResult{
			TrialID:       claim.TrialID,
			RequestHash:   requestHash,
			ExecutionHash: executionHash,
			Outcome:       "error",
			Category:      "result_invalid",
			Message:       fmt.Sprintf("Failed to parse artifacts: %v", err),
			Logs:          logs,
			DurationMs:    time.Since(startTime).Milliseconds(),
		}, nil
	}

	return TrialExecutionResult{
		TrialID:       claim.TrialID,
		RequestHash:   requestHash,
		ExecutionHash: executionHash,
		Outcome:       workerResult.Outcome,
		Category:      workerResult.Category,
		Message:       workerResult.Message,
		Events:        workerResult.Events,
		Artifacts:     artifacts,
		Trace:         workerResult.Trace,
		ExitCode:      exitCode,
		Logs:          logs,
		DurationMs:    time.Since(startTime).Milliseconds(),
	}, nil
}

// decodeWorkerArtifacts accepts both the current Artifact metadata list and
// the pre-protocol object form used by early local workers.
func decodeWorkerArtifacts(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]string{}, nil
	}
	var object map[string]string
	if err := json.Unmarshal(raw, &object); err == nil {
		return object, nil
	}
	var list []struct {
		Name        string `json:"name"`
		ContentHash string `json:"content_hash"`
		SHA256      string `json:"sha256"`
		Hash        string `json:"hash"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	result := make(map[string]string, len(list))
	for _, item := range list {
		if item.Name == "" {
			return nil, fmt.Errorf("artifact name is required")
		}
		hash := item.ContentHash
		if hash == "" {
			hash = item.SHA256
		}
		if hash == "" {
			hash = item.Hash
		}
		result[item.Name] = hash
	}
	return result, nil
}

// TrialExecutionResult holds the complete result of a trial execution.
type TrialExecutionResult struct {
	TrialID       string            `json:"trial_id"`
	RequestHash   string            `json:"request_hash"`
	ExecutionHash string            `json:"execution_hash"`
	Outcome       string            `json:"outcome"`
	Category      string            `json:"category,omitempty"`
	Message       string            `json:"message,omitempty"`
	Events        []map[string]any  `json:"events,omitempty"`
	Artifacts     map[string]string `json:"artifacts,omitempty"`
	Trace         map[string]any    `json:"trace,omitempty"`
	ExitCode      int64             `json:"exit_code"`
	Logs          string            `json:"logs,omitempty"`
	DurationMs    int64             `json:"duration_ms"`
}
