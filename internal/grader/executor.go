package grader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/xeipuuv/gojsonschema"
)

// Executor executes graders against trial results
type Executor struct {
	registry     *Registry
	artifactsDir string
}

// NewExecutor creates a new grader executor
func NewExecutor(registry *Registry, artifactsDir string) *Executor {
	return &Executor{
		registry:     registry,
		artifactsDir: artifactsDir,
	}
}

// Execute executes a grader against a trial
func (e *Executor) Execute(ctx context.Context, graderHash string, experimentID string, trialID string) (*GradeResult, error) {
	// Get grader from registry
	grader, err := e.registry.Get(graderHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get grader: %w", err)
	}

	// Get trial artifacts directory
	artifactsPath := filepath.Join(e.artifactsDir, experimentID, trialID)

	// Execute based on method
	switch grader.Spec.Method {
	case MethodFileContent:
		return e.executeFileContent(ctx, grader, artifactsPath)
	case MethodJSONSchema:
		return e.executeJSONSchema(ctx, grader, artifactsPath)
	case MethodCommandExitCode:
		return e.executeCommandExitCode(ctx, grader, artifactsPath)
	case MethodTraceAssertion:
		return e.executeTraceAssertion(ctx, grader, artifactsPath)
	case MethodLLMRubric:
		return e.executeLLMRubric(ctx, grader, artifactsPath)
	default:
		return nil, fmt.Errorf("unsupported grader method: %s", grader.Spec.Method)
	}
}

// executeFileContent checks if file content matches expected
func (e *Executor) executeFileContent(ctx context.Context, grader *Grader, artifactsPath string) (*GradeResult, error) {
	expectedFile, ok := grader.Spec.Config["expected_file"].(string)
	if !ok {
		return nil, fmt.Errorf("expected_file not found in config")
	}

	actualFilePattern, ok := grader.Spec.Config["actual_file_pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("actual_file_pattern not found in config")
	}

	// Read expected content
	expectedData, err := os.ReadFile(expectedFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read expected file: %w", err)
	}

	// Find actual file
	actualPath := filepath.Join(artifactsPath, actualFilePattern)
	actualData, err := os.ReadFile(actualPath)
	if err != nil {
		return &GradeResult{
			GraderID:   grader.Metadata.Name,
			GraderType: string(grader.Spec.Method),
			Passed:     false,
			Score:      grader.Spec.Scoring.FailedScore,
			Message:    fmt.Sprintf("failed to read actual file: %v", err),
			Evidence:   map[string]interface{}{"expected_file": expectedFile, "actual_file": actualPath},
			ExecutedAt: time.Now(),
		}, nil
	}

	// Compare content
	passed := string(expectedData) == string(actualData)
	score := grader.Spec.Scoring.FailedScore
	message := "content mismatch"
	if passed {
		score = grader.Spec.Scoring.PassedScore
		message = "content matches"
	}

	return &GradeResult{
		GraderID:   grader.Metadata.Name,
		GraderType: string(grader.Spec.Method),
		Passed:     passed,
		Score:      score,
		Message:    message,
		Evidence:   map[string]interface{}{"expected_file": expectedFile, "actual_file": actualPath},
		ExecutedAt: time.Now(),
	}, nil
}

// executeJSONSchema validates JSON against schema
func (e *Executor) executeJSONSchema(ctx context.Context, grader *Grader, artifactsPath string) (*GradeResult, error) {
	schemaPath, ok := grader.Spec.Config["schema_path"].(string)
	if !ok {
		return nil, fmt.Errorf("schema_path not found in config")
	}

	artifactPattern, ok := grader.Spec.Config["artifact_pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("artifact_pattern not found in config")
	}

	// Load JSON schema
	schemaLoader := gojsonschema.NewReferenceLoader("file://" + schemaPath)

	// Load artifact JSON
	artifactPath := filepath.Join(artifactsPath, artifactPattern)
	documentLoader := gojsonschema.NewReferenceLoader("file://" + artifactPath)

	// Validate
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return &GradeResult{
			GraderID:   grader.Metadata.Name,
			GraderType: string(grader.Spec.Method),
			Passed:     false,
			Score:      grader.Spec.Scoring.FailedScore,
			Message:    fmt.Sprintf("validation error: %v", err),
			Evidence:   map[string]interface{}{"schema_path": schemaPath, "artifact_path": artifactPath},
			ExecutedAt: time.Now(),
		}, nil
	}

	passed := result.Valid()
	score := grader.Spec.Scoring.FailedScore
	message := "schema validation failed"
	evidence := map[string]interface{}{
		"schema_path":   schemaPath,
		"artifact_path": artifactPath,
	}

	if passed {
		score = grader.Spec.Scoring.PassedScore
		message = "schema validation passed"
	} else {
		var errors []string
		for _, desc := range result.Errors() {
			errors = append(errors, desc.String())
		}
		evidence["errors"] = errors
		message = fmt.Sprintf("schema validation failed: %d errors", len(errors))
	}

	return &GradeResult{
		GraderID:   grader.Metadata.Name,
		GraderType: string(grader.Spec.Method),
		Passed:     passed,
		Score:      score,
		Message:    message,
		Evidence:   evidence,
		ExecutedAt: time.Now(),
	}, nil
}

// executeCommandExitCode executes a command and checks exit code
func (e *Executor) executeCommandExitCode(ctx context.Context, grader *Grader, artifactsPath string) (*GradeResult, error) {
	commandConfig, ok := grader.Spec.Config["command"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("command not found in config")
	}

	expectedExitCode, ok := grader.Spec.Config["expected_exit_code"].(int)
	if !ok {
		expectedExitCode = 0
	}

	timeoutSeconds, ok := grader.Spec.Config["timeout_seconds"].(int)
	if !ok {
		timeoutSeconds = 30
	}

	// Convert command to string slice
	var cmdArgs []string
	for _, arg := range commandConfig {
		argStr, ok := arg.(string)
		if !ok {
			return nil, fmt.Errorf("invalid command argument type")
		}
		// Replace {artifact_path} placeholder
		if argStr == "{artifact_path}" {
			argStr = artifactsPath
		}
		cmdArgs = append(cmdArgs, argStr)
	}

	if len(cmdArgs) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	// Create context with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Execute command
	cmd := exec.CommandContext(cmdCtx, cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()

	actualExitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			actualExitCode = exitErr.ExitCode()
		} else {
			return &GradeResult{
				GraderID:   grader.Metadata.Name,
				GraderType: string(grader.Spec.Method),
				Passed:     false,
				Score:      grader.Spec.Scoring.FailedScore,
				Message:    fmt.Sprintf("command execution error: %v", err),
				Evidence:   map[string]interface{}{"command": cmdArgs, "output": string(output)},
				ExecutedAt: time.Now(),
			}, nil
		}
	}

	passed := actualExitCode == expectedExitCode
	score := grader.Spec.Scoring.FailedScore
	message := fmt.Sprintf("exit code %d (expected %d)", actualExitCode, expectedExitCode)
	if passed {
		score = grader.Spec.Scoring.PassedScore
	}

	return &GradeResult{
		GraderID:   grader.Metadata.Name,
		GraderType: string(grader.Spec.Method),
		Passed:     passed,
		Score:      score,
		Message:    message,
		Evidence: map[string]interface{}{
			"command":           cmdArgs,
			"expected_exit_code": expectedExitCode,
			"actual_exit_code":   actualExitCode,
			"output":             string(output),
		},
		ExecutedAt: time.Now(),
	}, nil
}

// executeTraceAssertion checks trace events
func (e *Executor) executeTraceAssertion(ctx context.Context, grader *Grader, artifactsPath string) (*GradeResult, error) {
	// Read trace file
	tracePath := filepath.Join(artifactsPath, "trace.json")
	traceData, err := os.ReadFile(tracePath)
	if err != nil {
		return &GradeResult{
			GraderID:   grader.Metadata.Name,
			GraderType: string(grader.Spec.Method),
			Passed:     false,
			Score:      grader.Spec.Scoring.FailedScore,
			Message:    fmt.Sprintf("failed to read trace file: %v", err),
			Evidence:   map[string]interface{}{"trace_path": tracePath},
			ExecutedAt: time.Now(),
		}, nil
	}

	// Parse trace
	var trace map[string]interface{}
	if err := json.Unmarshal(traceData, &trace); err != nil {
		return &GradeResult{
			GraderID:   grader.Metadata.Name,
			GraderType: string(grader.Spec.Method),
			Passed:     false,
			Score:      grader.Spec.Scoring.FailedScore,
			Message:    fmt.Sprintf("failed to parse trace JSON: %v", err),
			Evidence:   map[string]interface{}{"trace_path": tracePath},
			ExecutedAt: time.Now(),
		}, nil
	}

	// Check assertions
	assertions, ok := grader.Spec.Config["assertions"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("assertions not found in config")
	}

	allPassed := true
	var failedAssertions []string

	for _, assertion := range assertions {
		assertionMap, ok := assertion.(map[string]interface{})
		if !ok {
			continue
		}

		assertionType, _ := assertionMap["type"].(string)

		switch assertionType {
		case "tool_called":
			toolName, _ := assertionMap["tool_name"].(string)
			// Simple check - look for tool name in trace
			if !containsToolCall(trace, toolName) {
				allPassed = false
				failedAssertions = append(failedAssertions, fmt.Sprintf("tool_called: %s", toolName))
			}
		case "skill_loaded":
			skillHash, _ := assertionMap["skill_hash"].(string)
			if !containsSkillLoad(trace, skillHash) {
				allPassed = false
				failedAssertions = append(failedAssertions, fmt.Sprintf("skill_loaded: %s", skillHash))
			}
		}
	}

	score := grader.Spec.Scoring.FailedScore
	message := "trace assertions failed"
	if allPassed {
		score = grader.Spec.Scoring.PassedScore
		message = "all trace assertions passed"
	}

	return &GradeResult{
		GraderID:   grader.Metadata.Name,
		GraderType: string(grader.Spec.Method),
		Passed:     allPassed,
		Score:      score,
		Message:    message,
		Evidence: map[string]interface{}{
			"trace_path":        tracePath,
			"failed_assertions": failedAssertions,
		},
		ExecutedAt: time.Now(),
	}, nil
}

// executeLLMRubric uses LLM to grade output (stub for M5)
func (e *Executor) executeLLMRubric(ctx context.Context, grader *Grader, artifactsPath string) (*GradeResult, error) {
	// M5: LLM Grader is interface only, not required for MVP
	return &GradeResult{
		GraderID:   grader.Metadata.Name,
		GraderType: string(grader.Spec.Method),
		Passed:     false,
		Score:      0.0,
		Message:    "LLM grader not implemented in M5",
		Evidence:   map[string]interface{}{"artifacts_path": artifactsPath},
		ExecutedAt: time.Now(),
	}, nil
}

// Helper functions

func containsToolCall(trace map[string]interface{}, toolName string) bool {
	// Simplified trace checking - actual implementation would parse trace structure
	traceStr, _ := json.Marshal(trace)
	return len(traceStr) > 0 // Placeholder
}

func containsSkillLoad(trace map[string]interface{}, skillHash string) bool {
	// Simplified trace checking - actual implementation would parse trace structure
	traceStr, _ := json.Marshal(trace)
	return len(traceStr) > 0 // Placeholder
}

// AggregateGrades aggregates multiple grade results into a single score
func AggregateGrades(results []GradeResult) float64 {
	if len(results) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, result := range results {
		sum += result.Score
	}

	return sum / float64(len(results))
}
