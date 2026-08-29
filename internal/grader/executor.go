package grader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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
	return e.ExecuteTrial(ctx, graderHash, experimentID, trialID, "")
}

// ExecuteTrial executes the grader for a specific case.  caseID is optional
// for backwards compatibility with callers that use a one-file Grader.
func (e *Executor) ExecuteTrial(ctx context.Context, graderHash string, experimentID string, trialID string, caseID string) (*GradeResult, error) {
	// Get grader from registry
	if e.registry == nil {
		return nil, fmt.Errorf("grader registry is nil")
	}
	grader, err := e.registry.Get(graderHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get grader: %w", err)
	}

	// Get trial artifacts directory
	artifactsPath := filepath.Join(e.artifactsDir, experimentID, trialID)
	if err := ensureContained(e.artifactsDir, artifactsPath); err != nil {
		return nil, err
	}
	if cases, ok := grader.Spec.Config["suite_cases"].([]any); ok && len(cases) > 0 {
		return e.executeSuiteAssertions(ctx, graderHash, grader, artifactsPath, caseID, cases)
	}

	// Execute based on method
	switch grader.Spec.Method {
	case MethodFileExists:
		return e.executeFileExists(ctx, graderHash, grader, artifactsPath)
	case MethodFileContent:
		return e.executeFileContent(ctx, graderHash, grader, artifactsPath)
	case MethodFileHash:
		return e.executeFileHash(ctx, graderHash, grader, artifactsPath)
	case MethodJSONSchema:
		return e.executeJSONSchema(ctx, graderHash, grader, artifactsPath)
	case MethodJSONField:
		return e.executeJSONField(ctx, graderHash, grader, artifactsPath)
	case MethodRegex:
		return e.executeRegex(ctx, graderHash, grader, artifactsPath)
	case MethodCommandExitCode:
		return e.executeCommandExitCode(ctx, graderHash, grader, artifactsPath)
	case MethodTraceAssertion:
		return e.executeTraceAssertion(ctx, graderHash, grader, artifactsPath)
	case MethodLLMRubric:
		return e.executeLLMRubric(ctx, graderHash, grader, artifactsPath)
	default:
		return nil, fmt.Errorf("unsupported grader method: %s", grader.Spec.Method)
	}
}

// executeFileContent checks if file content matches expected
func (e *Executor) executeFileContent(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
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
	actualPath, err := artifactPath(artifactsPath, actualFilePattern)
	if err != nil {
		return invalidResult(graderHash, grader, "invalid artifact path", map[string]interface{}{"path": actualFilePattern}), nil
	}
	actualData, err := os.ReadFile(actualPath)
	if err != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required artifact is missing: %v", err), map[string]interface{}{"expected_file": expectedFile, "actual_file": actualPath}), nil
	}

	// Compare content
	passed := string(expectedData) == string(actualData)
	score := grader.Spec.Scoring.FailedScore
	message := "content mismatch"
	if passed {
		score = grader.Spec.Scoring.PassedScore
		message = "content matches"
	}

	return newResult(graderHash, grader, statusFor(passed), passed, score, message, map[string]interface{}{"expected_file": expectedFile, "actual_file": actualPath, "artifact_hash": hashBytes(actualData)}), nil
}

// executeFileExists checks that a declared artifact exists and is a regular file.
func (e *Executor) executeFileExists(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	pattern, ok := stringConfig(grader.Spec.Config, "artifact_pattern", "actual_file_pattern", "path")
	if !ok || pattern == "" {
		return nil, fmt.Errorf("artifact path not found in config")
	}
	path, err := artifactPath(artifactsPath, pattern)
	if err != nil {
		return invalidResult(graderHash, grader, "invalid artifact path", map[string]interface{}{"path": pattern}), nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required artifact is missing: %v", statErr), map[string]interface{}{"artifact_path": path}), nil
	}
	passed := !info.IsDir()
	return newResult(graderHash, grader, statusFor(passed), passed, scoreFor(grader, passed), "artifact exists", map[string]interface{}{"artifact_path": path, "size_bytes": info.Size()}), nil
}

// executeFileHash validates a declared artifact content hash.
func (e *Executor) executeFileHash(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	pattern, ok := stringConfig(grader.Spec.Config, "artifact_pattern", "actual_file_pattern", "path")
	expected, okExpected := stringConfig(grader.Spec.Config, "expected_hash", "sha256")
	if !ok || !okExpected {
		return nil, fmt.Errorf("artifact_pattern and expected_hash are required")
	}
	path, err := artifactPath(artifactsPath, pattern)
	if err != nil {
		return invalidResult(graderHash, grader, "invalid artifact path", map[string]interface{}{"path": pattern}), nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required artifact is missing: %v", readErr), map[string]interface{}{"artifact_path": path}), nil
	}
	actual := hashBytes(data)
	passed := actual == expected
	return newResult(graderHash, grader, statusFor(passed), passed, scoreFor(grader, passed), fmt.Sprintf("artifact hash %s (expected %s)", actual, expected), map[string]interface{}{"artifact_path": path, "expected_hash": expected, "actual_hash": actual}), nil
}

// executeJSONSchema validates JSON against schema
func (e *Executor) executeJSONSchema(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
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
	resolvedArtifactPath, pathErr := artifactPath(artifactsPath, artifactPattern)
	if pathErr != nil {
		return invalidResult(graderHash, grader, "invalid artifact path", map[string]interface{}{"artifact_pattern": artifactPattern}), nil
	}
	if _, statErr := os.Stat(resolvedArtifactPath); statErr != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required artifact is missing: %v", statErr), map[string]interface{}{"schema_path": schemaPath, "artifact_path": resolvedArtifactPath}), nil
	}
	documentLoader := gojsonschema.NewReferenceLoader("file://" + resolvedArtifactPath)

	// Validate
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return newResult(graderHash, grader, "failed", false, grader.Spec.Scoring.FailedScore, fmt.Sprintf("validation error: %v", err), map[string]interface{}{"schema_path": schemaPath, "artifact_path": resolvedArtifactPath}), nil
	}

	passed := result.Valid()
	score := grader.Spec.Scoring.FailedScore
	message := "schema validation failed"
	evidence := map[string]interface{}{
		"schema_path":   schemaPath,
		"artifact_path": resolvedArtifactPath,
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

	return newResult(graderHash, grader, statusFor(passed), passed, score, message, evidence), nil
}

// executeCommandExitCode executes a command and checks exit code
func (e *Executor) executeCommandExitCode(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	commandConfig, ok := grader.Spec.Config["command"].([]interface{})
	if !ok {
		if values, okString := grader.Spec.Config["command"].([]string); okString {
			commandConfig = make([]interface{}, len(values))
			for i, value := range values {
				commandConfig[i] = value
			}
			ok = true
		}
	}
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
			return newResult(graderHash, grader, "failed", false, grader.Spec.Scoring.FailedScore, fmt.Sprintf("command execution error: %v", err), map[string]interface{}{"command": cmdArgs, "output": string(output)}), nil
		}
	}

	passed := actualExitCode == expectedExitCode
	score := grader.Spec.Scoring.FailedScore
	message := fmt.Sprintf("exit code %d (expected %d)", actualExitCode, expectedExitCode)
	if passed {
		score = grader.Spec.Scoring.PassedScore
	}

	return newResult(graderHash, grader, statusFor(passed), passed, score, message, map[string]interface{}{
		"command":            cmdArgs,
		"expected_exit_code": expectedExitCode,
		"actual_exit_code":   actualExitCode,
		"output":             string(output),
	}), nil
}

// executeTraceAssertion checks trace events
func (e *Executor) executeTraceAssertion(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	tracePath := filepath.Join(artifactsPath, "trace.json")
	traceData, err := os.ReadFile(tracePath)
	if os.IsNotExist(err) {
		// Older LangGraph workers used <trial>_trace.json. Accept it only as a
		// compatibility fallback while preserving the scoped artifact boundary.
		matches, _ := filepath.Glob(filepath.Join(artifactsPath, "*_trace.json"))
		if len(matches) == 1 {
			tracePath = matches[0]
			traceData, err = os.ReadFile(tracePath)
		}
	}
	if err != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required trace artifact is missing: %v", err), map[string]interface{}{"trace_path": tracePath}), nil
	}

	// Parse trace. Keep the generic value so both object and event-array traces
	// are accepted; the assertion evaluator walks either representation.
	var trace any
	if err := json.Unmarshal(traceData, &trace); err != nil {
		return newResult(graderHash, grader, "failed", false, grader.Spec.Scoring.FailedScore, fmt.Sprintf("failed to parse trace JSON: %v", err), map[string]interface{}{"trace_path": tracePath}), nil
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
		default:
			passed, reason := evaluateTraceExpression(trace, assertionType, assertionMap)
			if !passed {
				allPassed = false
				failedAssertions = append(failedAssertions, reason)
			}
		}
	}

	score := grader.Spec.Scoring.FailedScore
	message := "trace assertions failed"
	if allPassed {
		score = grader.Spec.Scoring.PassedScore
		message = "all trace assertions passed"
	}

	return newResult(graderHash, grader, statusFor(allPassed), allPassed, score, message, map[string]interface{}{
		"trace_path":        tracePath,
		"failed_assertions": failedAssertions,
		"trace_hash":        hashBytes(traceData),
	}), nil
}

// executeLLMRubric uses LLM to grade output (stub for M5)
func (e *Executor) executeLLMRubric(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	// A missing LLM judge is incomplete evidence, never a scored failure. This
	// prevents a disabled/unconfigured judge from silently depressing a trial to
	// zero while still blocking release promotion through evidence.complete.
	return incompleteResult(graderHash, grader, "LLM rubric grader is not configured", map[string]interface{}{"artifacts_path": artifactsPath}), nil
}

// executeJSONField compares selected JSON fields with a grader-only expected
// document. Fields use dotted paths and are compared structurally.
func (e *Executor) executeJSONField(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	pattern, ok := stringConfig(grader.Spec.Config, "artifact_pattern", "actual_file_pattern", "path")
	if !ok {
		return nil, fmt.Errorf("artifact_pattern not found in config")
	}
	actualPath, err := artifactPath(artifactsPath, pattern)
	if err != nil {
		return invalidResult(graderHash, grader, "invalid artifact path", map[string]interface{}{"path": pattern}), nil
	}
	actualBytes, err := os.ReadFile(actualPath)
	if err != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required artifact is missing: %v", err), map[string]interface{}{"artifact_path": actualPath}), nil
	}
	var actual any
	if err := json.Unmarshal(actualBytes, &actual); err != nil {
		return newResult(graderHash, grader, "failed", false, grader.Spec.Scoring.FailedScore, fmt.Sprintf("invalid JSON artifact: %v", err), map[string]interface{}{"artifact_path": actualPath}), nil
	}
	var expected any
	if expectedPath, ok := stringConfig(grader.Spec.Config, "expected_file", "expected_path"); ok {
		data, readErr := os.ReadFile(expectedPath)
		if readErr != nil {
			return invalidResult(graderHash, grader, fmt.Sprintf("expected file is unavailable: %v", readErr), map[string]interface{}{"expected_path": expectedPath}), nil
		}
		if err := json.Unmarshal(data, &expected); err != nil {
			return invalidResult(graderHash, grader, fmt.Sprintf("invalid expected JSON: %v", err), map[string]interface{}{"expected_path": expectedPath}), nil
		}
	} else if raw, ok := grader.Spec.Config["expected"].(map[string]interface{}); ok {
		expected = raw
	} else {
		return invalidResult(graderHash, grader, "expected_file or expected is required", nil), nil
	}
	fields := stringSliceConfig(grader.Spec.Config["fields"])
	if len(fields) == 0 {
		fields = []string{""}
	}
	failed := make([]string, 0)
	for _, field := range fields {
		av, aok := lookupJSON(actual, field)
		ev, eok := lookupJSON(expected, field)
		if !aok || !eok || !reflect.DeepEqual(av, ev) {
			failed = append(failed, field)
		}
	}
	passed := len(failed) == 0
	evidence := map[string]interface{}{"artifact_path": actualPath, "artifact_hash": hashBytes(actualBytes), "fields": fields}
	if len(failed) > 0 {
		evidence["failed_fields"] = failed
	}
	message := "selected JSON fields match"
	if !passed {
		message = fmt.Sprintf("selected JSON fields mismatch: %s", strings.Join(failed, ", "))
	}
	return newResult(graderHash, grader, statusFor(passed), passed, scoreFor(grader, passed), message, evidence), nil
}

func (e *Executor) executeRegex(ctx context.Context, graderHash string, grader *Grader, artifactsPath string) (*GradeResult, error) {
	pattern, ok := stringConfig(grader.Spec.Config, "artifact_pattern", "actual_file_pattern", "path")
	expression, okExpression := stringConfig(grader.Spec.Config, "regex", "pattern")
	if !ok || !okExpression {
		return nil, fmt.Errorf("artifact path and regex are required")
	}
	path, err := artifactPath(artifactsPath, pattern)
	if err != nil {
		return invalidResult(graderHash, grader, "invalid artifact path", map[string]interface{}{"path": pattern}), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return incompleteResult(graderHash, grader, fmt.Sprintf("required artifact is missing: %v", err), map[string]interface{}{"artifact_path": path}), nil
	}
	re, err := regexp.Compile(expression)
	if err != nil {
		return invalidResult(graderHash, grader, fmt.Sprintf("invalid regex: %v", err), map[string]interface{}{"regex": expression}), nil
	}
	passed := re.Match(data)
	return newResult(graderHash, grader, statusFor(passed), passed, scoreFor(grader, passed), "regex assertion evaluated", map[string]interface{}{"artifact_path": path, "regex": expression}), nil
}

// executeSuiteAssertions evaluates the assertions declared by an EvalSuite.
// Missing artifacts are explicitly marked incomplete, while present-but-wrong
// artifacts are scored failures. This distinction is consumed by the grading
// service and release gate to prevent missing evidence from becoming a zero.
func (e *Executor) executeSuiteAssertions(ctx context.Context, graderHash string, grader *Grader, artifactsPath, caseID string, cases []any) (*GradeResult, error) {
	var selected []map[string]any
	for _, raw := range cases {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["id"].(string)
		if caseID == "" || id == caseID {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return invalidResult(graderHash, grader, "suite case not found", map[string]interface{}{"case_id": caseID}), nil
	}
	passedCount, total := 0, 0
	missing, invalid := false, false
	failed := make([]string, 0)
	assertionEvidence := make([]map[string]interface{}, 0)
	for _, item := range selected {
		assertions, _ := item["assertions"].([]any)
		for _, raw := range assertions {
			a, ok := raw.(map[string]any)
			if !ok {
				invalid = true
				continue
			}
			total++
			id, _ := a["id"].(string)
			typ, _ := a["type"].(string)
			var result *GradeResult
			var err error
			switch typ {
			case "file_exists":
				result, err = e.runSuiteFileExists(graderHash, grader, artifactsPath, a)
			case "file_hash":
				result, err = e.runSuiteFileHash(graderHash, grader, artifactsPath, a)
			case "json_schema":
				result, err = e.runSuiteJSONSchema(graderHash, grader, artifactsPath, a)
			case "json_field":
				result, err = e.runSuiteJSONField(graderHash, grader, artifactsPath, a)
			case "regex":
				result, err = e.runSuiteRegex(graderHash, grader, artifactsPath, a)
			case "trace_assertion":
				result, err = e.runSuiteTrace(graderHash, grader, artifactsPath, a)
			default:
				invalid = true
			}
			if err != nil {
				invalid = true
				continue
			}
			itemEvidence := map[string]interface{}{"id": id, "type": typ, "status": result.Status, "passed": result.Passed, "message": result.Message}
			assertionEvidence = append(assertionEvidence, itemEvidence)
			switch result.Status {
			case "incomplete":
				missing = true
			case "invalid":
				invalid = true
			case "scored":
				if result.Passed {
					passedCount++
				} else {
					failed = append(failed, id)
				}
			}
		}
	}
	if total == 0 {
		return invalidResult(graderHash, grader, "suite case has no assertions", map[string]interface{}{"case_id": caseID}), nil
	}
	status := "scored"
	if invalid {
		status = "invalid"
	} else if missing {
		status = "incomplete"
	}
	passed := !missing && !invalid && passedCount == total
	score := grader.Spec.Scoring.FailedScore
	if passed {
		score = grader.Spec.Scoring.PassedScore
	} else if !missing && !invalid && total > 0 {
		ratio := float64(passedCount) / float64(total)
		score = grader.Spec.Scoring.FailedScore + ratio*(grader.Spec.Scoring.PassedScore-grader.Spec.Scoring.FailedScore)
	}
	message := "suite assertions passed"
	if status == "incomplete" {
		message = "suite evidence incomplete"
	} else if status == "invalid" {
		message = "suite assertion configuration invalid"
	} else if !passed {
		message = fmt.Sprintf("suite assertions failed: %s", strings.Join(failed, ", "))
	}
	return newResultWithStatus(graderHash, grader, status, passed, score, message, map[string]interface{}{"case_id": caseID, "assertions": assertionEvidence, "failed_assertions": failed}), nil
}

func (e *Executor) runSuiteFileExists(hash string, g *Grader, root string, a map[string]any) (*GradeResult, error) {
	path, ok := a["path"].(string)
	if !ok {
		return invalidResult(hash, g, "assertion path missing", nil), nil
	}
	p, err := artifactPath(root, path)
	if err != nil {
		return invalidResult(hash, g, "invalid artifact path", map[string]interface{}{"path": path}), nil
	}
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return incompleteResult(hash, g, "required artifact is missing", map[string]interface{}{"path": p}), nil
		}
		return newResult(hash, g, "failed", false, g.Spec.Scoring.FailedScore, err.Error(), map[string]interface{}{"path": p}), nil
	}
	return newResult(hash, g, "scored", !info.IsDir(), scoreFor(g, !info.IsDir()), "artifact exists", map[string]interface{}{"path": p}), nil
}

func (e *Executor) runSuiteFileHash(hash string, g *Grader, root string, a map[string]any) (*GradeResult, error) {
	path, _ := a["path"].(string)
	expected, _ := a["expectedHash"].(string)
	if expected == "" {
		expected, _ = a["sha256"].(string)
	}
	p, err := artifactPath(root, path)
	if err != nil || expected == "" {
		return invalidResult(hash, g, "file_hash requires path and expectedHash", map[string]interface{}{"path": path}), nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return incompleteResult(hash, g, "required artifact is missing", map[string]interface{}{"path": p}), nil
	}
	actual := hashBytes(b)
	ok := actual == expected
	return newResult(hash, g, statusFor(ok), ok, scoreFor(g, ok), "artifact hash checked", map[string]interface{}{"path": p, "expected_hash": expected, "actual_hash": actual}), nil
}

func (e *Executor) runSuiteJSONSchema(hash string, g *Grader, root string, a map[string]any) (*GradeResult, error) {
	path, _ := a["path"].(string)
	schema, _ := a["schemaRef"].(string)
	p, err := artifactPath(root, path)
	if err != nil || schema == "" {
		return invalidResult(hash, g, "json_schema requires path and schemaRef", map[string]interface{}{"path": path}), nil
	}
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return incompleteResult(hash, g, "required artifact is missing", map[string]interface{}{"path": p}), nil
		}
		return newResult(hash, g, "failed", false, g.Spec.Scoring.FailedScore, err.Error(), map[string]interface{}{"path": p}), nil
	}
	res, err := gojsonschema.Validate(gojsonschema.NewReferenceLoader("file://"+schema), gojsonschema.NewReferenceLoader("file://"+p))
	if err != nil {
		return newResult(hash, g, "failed", false, g.Spec.Scoring.FailedScore, err.Error(), map[string]interface{}{"path": p, "schema": schema}), nil
	}
	passed := res.Valid()
	ev := map[string]interface{}{"path": p, "schema": schema}
	if !passed {
		errs := make([]string, 0)
		for _, item := range res.Errors() {
			errs = append(errs, item.String())
		}
		ev["errors"] = errs
	}
	return newResult(hash, g, statusFor(passed), passed, scoreFor(g, passed), "JSON schema checked", ev), nil
}

func (e *Executor) runSuiteJSONField(hash string, g *Grader, root string, a map[string]any) (*GradeResult, error) {
	path, _ := a["path"].(string)
	expectedPath, _ := a["expectedRef"].(string)
	fields := stringSliceConfig(a["fields"])
	p, err := artifactPath(root, path)
	if err != nil || expectedPath == "" {
		return invalidResult(hash, g, "json_field requires path and expectedRef", map[string]interface{}{"path": path}), nil
	}
	actualBytes, err := os.ReadFile(p)
	if err != nil {
		return incompleteResult(hash, g, "required artifact is missing", map[string]interface{}{"path": p}), nil
	}
	expectedBytes, err := os.ReadFile(expectedPath)
	if err != nil {
		return invalidResult(hash, g, "expected reference is unavailable", map[string]interface{}{"expected_ref": expectedPath}), nil
	}
	var actual, expected any
	if json.Unmarshal(actualBytes, &actual) != nil || json.Unmarshal(expectedBytes, &expected) != nil {
		return newResult(hash, g, "failed", false, g.Spec.Scoring.FailedScore, "invalid JSON", map[string]interface{}{"path": p}), nil
	}
	failed := []string{}
	if len(fields) == 0 {
		fields = []string{""}
	}
	for _, field := range fields {
		av, aok := lookupJSON(actual, field)
		ev, eok := lookupJSON(expected, field)
		if !aok || !eok || !reflect.DeepEqual(av, ev) {
			failed = append(failed, field)
		}
	}
	ok := len(failed) == 0
	return newResult(hash, g, statusFor(ok), ok, scoreFor(g, ok), "JSON fields checked", map[string]interface{}{"path": p, "expected_ref": expectedPath, "fields": fields, "failed_fields": failed}), nil
}

func (e *Executor) runSuiteRegex(hash string, g *Grader, root string, a map[string]any) (*GradeResult, error) {
	path, _ := a["path"].(string)
	expression, _ := a["regex"].(string)
	if expression == "" {
		expression, _ = a["pattern"].(string)
	}
	p, err := artifactPath(root, path)
	if err != nil || expression == "" {
		return invalidResult(hash, g, "regex requires path and regex", nil), nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return incompleteResult(hash, g, "required artifact is missing", map[string]interface{}{"path": p}), nil
	}
	re, err := regexp.Compile(expression)
	if err != nil {
		return invalidResult(hash, g, err.Error(), nil), nil
	}
	ok := re.Match(b)
	return newResult(hash, g, statusFor(ok), ok, scoreFor(g, ok), "regex checked", map[string]interface{}{"path": p, "regex": expression}), nil
}

func (e *Executor) runSuiteTrace(hash string, g *Grader, root string, a map[string]any) (*GradeResult, error) {
	tracePath := filepath.Join(root, "trace.json")
	b, err := os.ReadFile(tracePath)
	if os.IsNotExist(err) {
		matches, _ := filepath.Glob(filepath.Join(root, "*_trace.json"))
		if len(matches) == 1 {
			tracePath = matches[0]
			b, err = os.ReadFile(tracePath)
		}
	}
	if err != nil {
		return incompleteResult(hash, g, "required trace artifact is missing", map[string]interface{}{"trace_path": tracePath}), nil
	}
	var trace any
	if json.Unmarshal(b, &trace) != nil {
		return newResult(hash, g, "failed", false, g.Spec.Scoring.FailedScore, "invalid trace JSON", map[string]interface{}{"trace_path": tracePath}), nil
	}
	assertion, _ := a["assertion"].(string)
	ok, reason := evaluateTraceExpression(trace, "", map[string]any{"assertion": assertion})
	return newResult(hash, g, statusFor(ok), ok, scoreFor(g, ok), reason, map[string]interface{}{"trace_path": tracePath, "trace_hash": hashBytes(b), "assertion": assertion}), nil
}

// Helper functions

func containsToolCall(trace any, toolName string) bool {
	if toolName == "" {
		return false
	}
	found := false
	walkTrace(trace, func(key string, value any, parent map[string]any) {
		lk := strings.ToLower(key)
		if lk != "tool" && lk != "tool_name" && lk != "name" {
			return
		}
		name, ok := value.(string)
		if ok && name == toolName {
			found = true
		}
	})
	return found
}

func containsSkillLoad(trace any, skillHash string) bool {
	found := false
	walkTrace(trace, func(key string, value any, parent map[string]any) {
		if key != "step" && key != "event_type" && key != "type" && key != "name" {
			return
		}
		name, _ := value.(string)
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "skill_loaded") && !strings.Contains(lower, "skill loaded") && lower != "load_skill" {
			return
		}
		if skillHash == "" {
			found = true
			return
		}
		blob, _ := json.Marshal(parent)
		if strings.Contains(string(blob), skillHash) || strings.Contains(string(blob), strings.TrimPrefix(skillHash, "sha256:")) {
			found = true
			return
		}
		// Suite trigger assertions commonly identify a skill by logical name.
		if strings.Contains(string(blob), skillHash) {
			found = true
		}
	})
	return found
}

func evaluateTraceExpression(trace any, _ string, assertion map[string]any) (bool, string) {
	expr, _ := assertion["assertion"].(string)
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false, "trace assertion is empty"
	}
	if strings.HasPrefix(expr, "skill_loaded:") {
		name := strings.TrimPrefix(expr, "skill_loaded:")
		return containsSkillLoad(trace, name), expr
	}
	if strings.HasPrefix(expr, "skill_not_loaded:") {
		name := strings.TrimPrefix(expr, "skill_not_loaded:")
		return !containsSkillLoad(trace, name), expr
	}
	if strings.HasPrefix(expr, "tool_denied:") {
		target := strings.TrimPrefix(expr, "tool_denied:")
		return traceHasDecision(trace, target, "deny"), expr
	}
	if strings.HasPrefix(expr, "no_read_under:") {
		prefix := strings.TrimPrefix(expr, "no_read_under:")
		return !traceHasReadUnder(trace, prefix), expr
	}
	if strings.HasPrefix(expr, "reads_only_declared_fixture:") {
		path := strings.TrimPrefix(expr, "reads_only_declared_fixture:")
		return traceHasReadPath(trace, path), expr
	}
	if strings.HasPrefix(expr, "agent_did_not_read:") {
		prefix := strings.TrimPrefix(expr, "agent_did_not_read:")
		return !traceHasReadUnder(trace, prefix), expr
	}
	if strings.HasPrefix(expr, "no_read_paths:") {
		raw := strings.TrimPrefix(expr, "no_read_paths:")
		raw = strings.Trim(raw, "[]")
		for _, p := range strings.Split(raw, ",") {
			p = strings.Trim(strings.TrimSpace(p), "\"")
			if p != "" && traceHasReadUnder(trace, p) {
				return false, expr
			}
		}
		return true, expr
	}
	if strings.HasPrefix(expr, "paths_confined_to:") {
		root := strings.TrimPrefix(expr, "paths_confined_to:")
		return tracePathsConfined(trace, root), expr
	}
	return false, "unsupported trace assertion: " + expr
}

func walkTrace(value any, fn func(string, any, map[string]any)) {
	var walk func(any, map[string]any)
	walk = func(v any, parent map[string]any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				fn(k, child, x)
				walk(child, x)
			}
		case []any:
			for _, child := range x {
				walk(child, parent)
			}
		}
	}
	walk(value, nil)
}

func traceHasDecision(trace any, target, decision string) bool {
	found := false
	walkTrace(trace, func(key string, value any, parent map[string]any) {
		if strings.EqualFold(key, "decision") {
			if d, _ := value.(string); strings.EqualFold(d, decision) {
				b, _ := json.Marshal(parent)
				if target == "" || strings.Contains(strings.ToLower(string(b)), strings.ToLower(target)) {
					found = true
				}
			}
		}
	})
	return found
}
func traceHasReadUnder(trace any, prefix string) bool {
	found := false
	walkTrace(trace, func(key string, value any, parent map[string]any) {
		lk := strings.ToLower(key)
		if lk != "target" && lk != "path" && lk != "file" && lk != "name" {
			return
		}
		s, _ := value.(string)
		if strings.Contains(strings.ToLower(s), strings.ToLower(prefix)) {
			b, _ := json.Marshal(parent)
			blob := strings.ToLower(string(b))
			if strings.Contains(blob, "read") || strings.Contains(blob, "filesystem") || strings.Contains(blob, "path") {
				found = true
			}
		}
	})
	return found
}
func traceHasReadPath(trace any, path string) bool { return traceHasReadUnder(trace, path) }
func tracePathsConfined(trace any, root string) bool {
	ok := true
	walkTrace(trace, func(key string, value any, parent map[string]any) {
		if key != "target" && key != "path" && key != "file" {
			return
		}
		s, _ := value.(string)
		if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, root) {
			b, _ := json.Marshal(parent)
			if strings.Contains(strings.ToLower(string(b)), "read") || strings.Contains(strings.ToLower(string(b)), "path") {
				ok = false
			}
		}
	})
	return ok
}

func ensureContained(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact path escapes configured root")
	}
	return nil
}
func artifactPath(root, pattern string) (string, error) {
	if pattern == "" || filepath.IsAbs(pattern) {
		return "", fmt.Errorf("artifact path must be relative")
	}
	path := filepath.Join(root, filepath.FromSlash(pattern))
	if err := ensureContained(root, path); err != nil {
		return "", err
	}
	// Lexical traversal checks do not catch a symlink inside the artifact tree.
	// Resolve the existing portion and reject links that point outside the root
	// (or any symlink artifact itself), including for missing-leaf paths.
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	probe := path
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			if info, lstatErr := os.Lstat(probe); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("artifact path cannot contain symlink")
			}
			resolved, resolveErr := filepath.EvalSymlinks(probe)
			if resolveErr != nil {
				return "", resolveErr
			}
			if err := ensureContained(rootReal, resolved); err != nil {
				return "", fmt.Errorf("artifact path resolves outside configured root")
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return path, nil
}
func stringConfig(config map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := config[key].(string); ok && value != "" {
			return value, true
		}
	}
	return "", false
}
func stringSliceConfig(value any) []string {
	var out []string
	switch v := value.(type) {
	case []string:
		return append(out, v...)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func lookupJSON(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func statusFor(passed bool) string { return "scored" }
func scoreFor(g *Grader, passed bool) float64 {
	if passed {
		return g.Spec.Scoring.PassedScore
	}
	return g.Spec.Scoring.FailedScore
}
func newResult(hash string, g *Grader, status string, passed bool, score float64, message string, evidence map[string]interface{}) *GradeResult {
	return newResultWithStatus(hash, g, status, passed, score, message, evidence)
}
func newResultWithStatus(hash string, g *Grader, status string, passed bool, score float64, message string, evidence map[string]interface{}) *GradeResult {
	if evidence == nil {
		evidence = map[string]interface{}{}
	}
	result := &GradeResult{GraderID: g.Metadata.Name, GraderHash: hash, GraderVersion: g.Metadata.Version, GraderType: string(g.Spec.Method), Status: status, Passed: passed, Score: score, Message: message, Evidence: evidence, ExecutedAt: time.Now().UTC()}
	if encoded, err := json.Marshal(evidence); err == nil {
		result.EvidenceHash = hashBytes(encoded)
		// Until the worker supplies a separate request/input manifest, the
		// canonical evidence projection is the immutable grading input identity.
		// Keeping both fields populated prevents untraceable grades while leaving
		// room for a distinct input hash in the protocol later.
		result.InputHash = result.EvidenceHash
	}
	return result
}
func incompleteResult(hash string, g *Grader, message string, evidence map[string]interface{}) *GradeResult {
	return newResultWithStatus(hash, g, "incomplete", false, 0, message, evidence)
}
func invalidResult(hash string, g *Grader, message string, evidence map[string]interface{}) *GradeResult {
	return newResultWithStatus(hash, g, "invalid", false, 0, message, evidence)
}

// AggregateGrades aggregates multiple grade results into a single score
func AggregateGrades(results []GradeResult) float64 {
	if len(results) == 0 {
		return 0.0
	}

	sum := 0.0
	valid := 0
	for _, result := range results {
		if result.Status != "" && result.Status != "scored" {
			continue
		}
		sum += result.Score
		valid++
	}

	if valid == 0 {
		return 0
	}
	return sum / float64(valid)
}
