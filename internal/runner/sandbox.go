package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// SandboxConfig holds configuration for a trial sandbox
type SandboxConfig struct {
	Image           string
	CASRoot         string
	ArtifactsRoot   string
	Env             []string
	MemoryLimitMB   int64
	CPUQuota        int64
	NetworkDisabled bool
	ReadOnlyRoot    bool
	User            string
}

// Sandbox manages a Docker container for trial execution
type Sandbox struct {
	cli         *client.Client
	containerID string
	config      SandboxConfig
}

// NewSandbox creates a new sandbox instance
func NewSandbox(cfg SandboxConfig) (*Sandbox, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Sandbox{
		cli:    cli,
		config: cfg,
	}, nil
}

// Create creates and starts the container
func (s *Sandbox) Create(ctx context.Context) error {
	// Avoid an unconditional registry request for every trial. This keeps
	// offline/local runs deterministic while retaining the convenience of
	// pulling a missing image on first use.
	if _, err := s.cli.ImageInspect(ctx, s.config.Image); err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to inspect image %q: %w", s.config.Image, err)
		}
		reader, pullErr := s.cli.ImagePull(ctx, s.config.Image, image.PullOptions{})
		if pullErr != nil {
			return fmt.Errorf("failed to pull image: %w", pullErr)
		}
		if _, copyErr := io.Copy(io.Discard, reader); copyErr != nil {
			_ = reader.Close()
			return fmt.Errorf("failed to read image pull response: %w", copyErr)
		}
		if closeErr := reader.Close(); closeErr != nil {
			return fmt.Errorf("failed to close image pull response: %w", closeErr)
		}
	}

	// Configure mounts
	mounts := []mount.Mount{
		{
			Type:     mount.TypeBind,
			Source:   s.config.CASRoot,
			Target:   "/cas",
			ReadOnly: true,
		},
		{
			Type:   mount.TypeBind,
			Source: s.config.ArtifactsRoot,
			Target: "/artifacts",
		},
	}

	// Configure container resources and security
	hostConfig := &container.HostConfig{
		Mounts:         mounts,
		NetworkMode:    container.NetworkMode("none"),
		ReadonlyRootfs: s.config.ReadOnlyRoot,
		Resources: container.Resources{
			Memory:   s.config.MemoryLimitMB * 1024 * 1024,
			CPUQuota: s.config.CPUQuota,
		},
		AutoRemove: false, // Manual cleanup for proper error handling
	}

	if !s.config.NetworkDisabled {
		hostConfig.NetworkMode = container.NetworkMode("bridge")
	}

	containerConfig := &container.Config{
		Image: s.config.Image,
		Env:   s.config.Env,
		User:  s.config.User,
		Cmd:   []string{"tail", "-f", "/dev/null"}, // Keep alive for exec
	}

	// Create container
	resp, err := s.cli.ContainerCreate(ctx, containerConfig, hostConfig, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	s.containerID = resp.ID

	// Start container
	if err := s.cli.ContainerStart(ctx, s.containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// Exec executes a command inside the container
func (s *Sandbox) Exec(ctx context.Context, cmd []string) (exitCode int, err error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := s.cli.ContainerExecCreate(ctx, s.containerID, execConfig)
	if err != nil {
		return -1, fmt.Errorf("failed to create exec: %w", err)
	}

	if err := s.cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
		return -1, fmt.Errorf("failed to start exec: %w", err)
	}

	// Wait for completion
	for {
		inspect, err := s.cli.ContainerExecInspect(ctx, execID.ID)
		if err != nil {
			return -1, fmt.Errorf("failed to inspect exec: %w", err)
		}
		if !inspect.Running {
			return inspect.ExitCode, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Stop stops the container
func (s *Sandbox) Stop(ctx context.Context) error {
	timeout := 10
	return s.cli.ContainerStop(ctx, s.containerID, container.StopOptions{Timeout: &timeout})
}

// Remove removes the container
func (s *Sandbox) Remove(ctx context.Context) error {
	return s.cli.ContainerRemove(ctx, s.containerID, container.RemoveOptions{Force: true})
}

// Cleanup stops and removes the container, ensuring no residue
func (s *Sandbox) Cleanup(ctx context.Context) error {
	// Try to stop first
	if err := s.Stop(ctx); err != nil {
		// Container might already be stopped, continue to remove
		if !strings.Contains(err.Error(), "is not running") {
			return fmt.Errorf("failed to stop container: %w", err)
		}
	}

	// Remove container
	if err := s.Remove(ctx); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// Close closes the Docker client
func (s *Sandbox) Close() error {
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}

// PrepareWorkspace creates a trial-specific workspace directory
func PrepareWorkspace(baseDir, trialID string) (string, error) {
	workspaceDir := fmt.Sprintf("%s/%s", baseDir, trialID)
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create workspace: %w", err)
	}
	return workspaceDir, nil
}

// RunTrial executes a trial in the sandbox and returns exit code and logs
func RunTrial(ctx context.Context, config SandboxConfig) (exitCode int64, logs string, err error) {
	sandbox, err := NewSandbox(config)
	if err != nil {
		return -1, "", fmt.Errorf("failed to create sandbox: %w", err)
	}
	defer sandbox.Close()

	if err := sandbox.Create(ctx); err != nil {
		return -1, "", fmt.Errorf("failed to create container: %w", err)
	}
	defer sandbox.Cleanup(ctx)

	// The request is mounted at /artifacts and the worker's unified module
	// supports a one-shot local mode for Go-managed sandboxes.
	code, err := sandbox.Exec(ctx, []string{"python", "-m", "langgraph_worker", "--request", "/artifacts/request.json", "--cas", "/cas", "--artifacts", "/artifacts"})
	if err != nil {
		return int64(code), "", err
	}

	// TODO: Capture actual logs from container
	logs = "Worker execution completed"

	return int64(code), logs, nil
}
