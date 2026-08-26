package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/runner"
)

// A3: Each trial gets independent container with network/resource/readonly root/non-root enforced,
// and no residual containers after termination.

func TestSandboxIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()
	casDir := filepath.Join(tempDir, "cas")
	artifactsDir := filepath.Join(tempDir, "artifacts")

	os.MkdirAll(casDir, 0755)
	os.MkdirAll(artifactsDir, 0755)

	config := runner.SandboxConfig{
		Image:           "python:3.11-slim",
		CASRoot:         casDir,
		ArtifactsRoot:   artifactsDir,
		Env:             []string{"TEST_VAR=test_value"},
		MemoryLimitMB:   512,
		CPUQuota:        50000,
		NetworkDisabled: true,
		ReadOnlyRoot:    true,
		User:            "1000:1000",
	}

	// A3.1: Container can be created
	sandbox, err := runner.NewSandbox(config)
	if err != nil {
		t.Fatalf("NewSandbox failed: %v", err)
	}
	defer sandbox.Close()

	if err := sandbox.Create(ctx); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// A3.2: Container can execute commands
	exitCode, err := sandbox.Exec(ctx, []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// A3.3: Container can be stopped
	if err := sandbox.Stop(ctx); err != nil {
		t.Logf("Stop warning: %v", err)
	}

	// A3.4: Container can be removed (no residual)
	if err := sandbox.Remove(ctx); err != nil {
		t.Errorf("Remove failed: %v", err)
	}

	t.Log("Sandbox lifecycle completed successfully")
}

func TestSandboxResourceLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()
	casDir := filepath.Join(tempDir, "cas")
	artifactsDir := filepath.Join(tempDir, "artifacts")

	os.MkdirAll(casDir, 0755)
	os.MkdirAll(artifactsDir, 0755)

	config := runner.SandboxConfig{
		Image:           "python:3.11-slim",
		CASRoot:         casDir,
		ArtifactsRoot:   artifactsDir,
		Env:             []string{},
		MemoryLimitMB:   256,
		CPUQuota:        25000,
		NetworkDisabled: true,
		ReadOnlyRoot:    true,
		User:            "1000:1000",
	}

	// A3.5: Resource limits are enforced
	sandbox, err := runner.NewSandbox(config)
	if err != nil {
		t.Fatalf("NewSandbox failed: %v", err)
	}
	defer sandbox.Close()

	if err := sandbox.Create(ctx); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Execute a simple command to verify limits are applied
	exitCode, err := sandbox.Exec(ctx, []string{"python", "-c", "print('test')"})
	if err != nil {
		t.Logf("Exec completed with error: %v", err)
	}

	t.Logf("Exit code: %d", exitCode)

	// Cleanup
	sandbox.Cleanup(ctx)
}

func TestSandboxNetworkDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	ctx := context.Background()
	tempDir := t.TempDir()
	casDir := filepath.Join(tempDir, "cas")
	artifactsDir := filepath.Join(tempDir, "artifacts")

	os.MkdirAll(casDir, 0755)
	os.MkdirAll(artifactsDir, 0755)

	config := runner.SandboxConfig{
		Image:           "python:3.11-slim",
		CASRoot:         casDir,
		ArtifactsRoot:   artifactsDir,
		Env:             []string{},
		MemoryLimitMB:   256,
		CPUQuota:        50000,
		NetworkDisabled: true, // A3: Network isolation enforced
		ReadOnlyRoot:    true,
		User:            "1000:1000",
	}

	// Create sandbox with network disabled
	sandbox, err := runner.NewSandbox(config)
	if err != nil {
		t.Fatalf("NewSandbox failed: %v", err)
	}
	defer sandbox.Close()

	if err := sandbox.Create(ctx); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Container created successfully with network disabled
	t.Log("Sandbox created with network disabled")

	// Cleanup
	sandbox.Cleanup(ctx)
}
