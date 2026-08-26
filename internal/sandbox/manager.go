package sandbox

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Manager manages Docker sandbox containers for trial execution.
type Manager struct {
	cli        *client.Client
	casRoot    string // CAS root for skill content
	outputRoot string // host path for artifacts
}

// NewManager creates a Docker-backed sandbox manager.
func NewManager(casRoot, outputRoot string) (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &Manager{cli: cli, casRoot: casRoot, outputRoot: outputRoot}, nil
}

// SandboxConfig describes a trial sandbox.
type SandboxConfig struct {
	TrialID     string
	Image       string
	SkillHash   string // empty for without_skill
	Env         map[string]string
	CPULimit    int64 // milli-CPUs (e.g. 1000 = 1 CPU)
	MemoryLimit int64 // bytes
}

// Container represents a running sandbox.
type Container struct {
	ID      string
	TrialID string
	manager *Manager
}

// Start creates and starts a sandbox container.
func (m *Manager) Start(ctx context.Context, cfg SandboxConfig) (*Container, error) {
	containerName := fmt.Sprintf("skillgate-trial-%s", cfg.TrialID)

	// Mount CAS as read-only for skill content
	mounts := []mount.Mount{
		{
			Type:     mount.TypeBind,
			Source:   m.casRoot,
			Target:   "/cas",
			ReadOnly: true,
		},
	}

	// Mount output directory for artifacts
	outputDir := filepath.Join(m.outputRoot, cfg.TrialID)
	mounts = append(mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: outputDir,
		Target: "/output",
	})

	env := []string{}
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	if cfg.SkillHash != "" {
		env = append(env, fmt.Sprintf("SKILL_HASH=%s", cfg.SkillHash))
	}

	hostConfig := &container.HostConfig{
		Mounts:      mounts,
		NetworkMode: "none", // network isolation
		ReadonlyRootfs: true,
		Resources: container.Resources{
			NanoCPUs: cfg.CPULimit * 1_000_000, // convert milli-CPU to nano-CPU
			Memory:   cfg.MemoryLimit,
		},
	}

	// Run as non-root user (uid 1000)
	config := &container.Config{
		Image: cfg.Image,
		Env:   env,
		User:  "1000:1000",
	}

	resp, err := m.cli.ContainerCreate(ctx, config, hostConfig, &network.NetworkingConfig{}, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := m.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return &Container{ID: resp.ID, TrialID: cfg.TrialID, manager: m}, nil
}

// Wait blocks until the container exits and returns the exit code.
func (c *Container) Wait(ctx context.Context) (int64, error) {
	statusCh, errCh := c.manager.cli.ContainerWait(ctx, c.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return -1, err
		}
	case status := <-statusCh:
		return status.StatusCode, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	return 0, nil
}

// Logs retrieves container logs.
func (c *Container) Logs(ctx context.Context) (io.ReadCloser, error) {
	return c.manager.cli.ContainerLogs(ctx, c.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
	})
}

// Stop stops and removes the container.
func (c *Container) Stop(ctx context.Context) error {
	timeout := 10 * time.Second
	if err := c.manager.cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	if err := c.manager.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

// Close releases Docker client resources.
func (m *Manager) Close() error {
	return m.cli.Close()
}
