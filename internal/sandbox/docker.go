package sandbox

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// DockerManager manages Docker sandbox containers for trial execution.
type DockerManager struct {
	cli    *client.Client
	config Config
}

// Config defines sandbox container configuration.
type Config struct {
	Image           string
	Network         string // "none" for isolated
	CPUQuota        int64  // CPU limit in microseconds per period
	MemoryLimitMB   int64
	ReadOnlyRootFS  bool
	RunAsNonRoot    bool
	TimeoutDuration time.Duration
}

// NewDockerManager creates a Docker sandbox manager.
func NewDockerManager(config Config) (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Set defaults
	if config.Image == "" {
		config.Image = "skillgate-langgraph-worker:latest"
	}
	if config.Network == "" {
		config.Network = "none"
	}
	if config.MemoryLimitMB == 0 {
		config.MemoryLimitMB = 512
	}
	if config.TimeoutDuration == 0 {
		config.TimeoutDuration = 5 * time.Minute
	}

	return &DockerManager{
		cli:    cli,
		config: config,
	}, nil
}

// RunOptions defines per-trial container options.
type RunOptions struct {
	TrialID     string
	Mounts      []Mount
	Env         []string
	WorkDir     string
	Entrypoint  []string
	Cmd         []string
}

// Mount defines a volume mount.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// RunResult contains execution results.
type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// Run executes a trial in an isolated Docker container.
func (m *DockerManager) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	containerName := fmt.Sprintf("trial-%s", opts.TrialID)

	// Convert mounts
	mounts := make([]mount.Mount, len(opts.Mounts))
	for i, mt := range opts.Mounts {
		mounts[i] = mount.Mount{
			Type:     mount.TypeBind,
			Source:   mt.Source,
			Target:   mt.Target,
			ReadOnly: mt.ReadOnly,
		}
	}

	// Container config
	containerConfig := &container.Config{
		Image:      m.config.Image,
		Env:        opts.Env,
		WorkingDir: opts.WorkDir,
		User:       "1000:1000", // non-root if enabled
		Labels: map[string]string{
			"skillgate.trial_id": opts.TrialID,
			"skillgate.component": "langgraph-worker",
		},
	}

	if len(opts.Entrypoint) > 0 {
		containerConfig.Entrypoint = opts.Entrypoint
	}
	if len(opts.Cmd) > 0 {
		containerConfig.Cmd = opts.Cmd
	}

	// Host config
	hostConfig := &container.HostConfig{
		Mounts:         mounts,
		NetworkMode:    container.NetworkMode(m.config.Network),
		ReadonlyRootfs: m.config.ReadOnlyRootFS,
		Resources: container.Resources{
			Memory:   m.config.MemoryLimitMB * 1024 * 1024,
			CPUQuota: m.config.CPUQuota,
		},
		AutoRemove: true, // cleanup on exit
	}

	// Network config
	networkConfig := &network.NetworkingConfig{}

	// Create container
	resp, err := m.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	containerID := resp.ID

	// Ensure cleanup on any error
	defer func() {
		// Best effort removal if still exists
		_ = m.cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{Force: true})
	}()

	// Start container
	startTime := time.Now()
	if err := m.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for completion with timeout
	waitCtx, cancel := context.WithTimeout(ctx, m.config.TimeoutDuration)
	defer cancel()

	statusCh, errCh := m.cli.ContainerWait(waitCtx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("container wait failed: %w", err)
		}
	case status := <-statusCh:
		duration := time.Since(startTime)

		// Collect logs
		logs, err := m.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get logs: %w", err)
		}
		defer logs.Close()

		logBytes, _ := io.ReadAll(logs)

		return &RunResult{
			ExitCode: int(status.StatusCode),
			Stdout:   string(logBytes), // stdout/stderr are combined
			Stderr:   "",
			Duration: duration,
		}, nil
	case <-waitCtx.Done():
		// Timeout: force kill
		_ = m.cli.ContainerKill(context.Background(), containerID, "SIGKILL")
		return nil, fmt.Errorf("trial execution timeout after %v", m.config.TimeoutDuration)
	}

	return nil, fmt.Errorf("unexpected wait completion")
}

// Stop forcefully terminates a running container.
func (m *DockerManager) Stop(ctx context.Context, trialID string) error {
	containerName := fmt.Sprintf("trial-%s", trialID)
	timeout := 5 // seconds
	return m.cli.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout})
}

// Close closes the Docker client.
func (m *DockerManager) Close() error {
	return m.cli.Close()
}
