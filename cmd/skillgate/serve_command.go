package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/execution"
	"github.com/Lin-xun1113/SkillGate/internal/grader"
	"github.com/Lin-xun1113/SkillGate/internal/grading"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
)

func serveCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	grpcAddr := flagValue(args, "--grpc-addr", ":50051")
	artifactsDir := flagValue(args, "--artifacts-dir", "./artifacts")
	gradersDir := flagValue(args, "--graders-dir", "./graders")
	projectRoot := flagValue(args, "--project-root", os.Getenv("SKILLGATE_PROJECT_ROOT"))
	var err error
	if projectRoot == "" {
		projectRoot, err = os.Getwd()
		if err != nil {
			return returnWithError(jsonOutput, "IO_ERROR", "project-root", fmt.Sprintf("failed to resolve project root: %v", err))
		}
	}
	casDir := flagValue(args, "--cas-dir", os.Getenv("SKILLGATE_CAS_DIR"))
	if casDir == "" {
		casDir = filepath.Join(projectRoot, ".skillgate", "cas")
	}
	pollIntervalStr := flagValue(args, "--grading-poll-interval", "5s")
	sweepIntervalStr := flagValue(args, "--sweep-interval", pollIntervalStr)
	sweepLimit := intFlag(args, "--sweep-limit", 100)

	pollInterval, err := time.ParseDuration(pollIntervalStr)
	if err != nil {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "grading-poll-interval", fmt.Sprintf("invalid duration: %v", err))
	}
	sweepInterval, err := time.ParseDuration(sweepIntervalStr)
	if err != nil || sweepInterval <= 0 {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "sweep-interval", fmt.Sprintf("invalid duration: %q", sweepIntervalStr))
	}
	if sweepLimit < 1 || sweepLimit > 1000 {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "sweep-limit", "sweep-limit 必须位于 1..1000")
	}

	dbURL, err := databaseURL(args)
	if err != nil {
		return returnSchedulerError(jsonOutput, "database", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgStore, err := postgres.Open(ctx, dbURL)
	if err != nil {
		return returnSchedulerError(jsonOutput, "database", err)
	}
	defer pgStore.Close()

	// Initialize Grader Registry
	graderRegistry := grader.NewRegistry(gradersDir)
	if err := graderRegistry.LoadFromDirectory(gradersDir); err != nil {
		log.Printf("Warning: failed to load graders from %s: %v", gradersDir, err)
	}

	// Initialize Grading Service
	gradingService := grading.NewService(pgStore, graderRegistry, artifactsDir, pollInterval)

	// Project execution content from the persisted manifest before handing a
	// claim to a worker. This keeps the worker a thin protocol client.
	compiler := manifest.NewCompiler(projectRoot, casDir)
	projector := execution.NewProjector(compiler)
	server := runner.NewServer(pgStore, &runner.ServiceOptions{
		Projector:     projector,
		ArtifactsRoot: artifactsDir,
	})

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", grpcAddr, fmt.Sprintf("failed to listen on %s: %v", grpcAddr, err))
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create context for grading poller
	pollerCtx, pollerCancel := context.WithCancel(context.Background())
	defer pollerCancel()

	// Start grading poller
	go startGradingPoller(pollerCtx, gradingService, pgStore, pollInterval)
	// Reconcile expired leases and budget-deadline work in the same long-lived
	// control-plane process. This is independent from grading so a stalled
	// grader cannot prevent scheduler recovery.
	go startSchedulerSweeper(pollerCtx, pgStore, sweepInterval, sweepLimit)

	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.Serve(lis)
	}()

	if jsonOutput {
		printSuccess(true, map[string]any{
			"started":        true,
			"grpc_addr":      lis.Addr().String(),
			"database":       "connected",
			"grading_poller": "started",
			"poll_interval":  pollInterval.String(),
			"sweep_interval": sweepInterval.String(),
			"sweep_limit":    sweepLimit,
			"artifacts_dir":  artifactsDir,
			"graders_dir":    gradersDir,
			"project_root":   projectRoot,
			"cas_dir":        casDir,
		})
	} else {
		fmt.Printf("skillgate runner server listening on %s\n", lis.Addr().String())
		fmt.Printf("grading poller started (interval: %v); scheduler sweeper started (interval: %v)\n", pollInterval, sweepInterval)
	}

	select {
	case sig := <-sigChan:
		if !jsonOutput {
			fmt.Printf("received signal %v, gracefully stopping server...\n", sig)
		}
		pollerCancel()
		server.Stop()
		return 0
	case err := <-serverErrChan:
		pollerCancel()
		if err != nil {
			return returnWithError(jsonOutput, "IO_ERROR", grpcAddr, fmt.Sprintf("server error: %v", err))
		}
		return 0
	}
}

// startGradingPoller polls for experiments in GRADING status and processes them
func startGradingPoller(ctx context.Context, service *grading.Service, store interface {
	GetExperimentsInGrading(context.Context) ([]string, error)
}, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Println("Grading Poller started")

	for {
		select {
		case <-ctx.Done():
			log.Println("Grading Poller stopped")
			return
		case <-ticker.C:
			// Get all experiments in GRADING status
			experiments, err := store.GetExperimentsInGrading(ctx)
			if err != nil {
				log.Printf("Failed to query grading experiments: %v", err)
				continue
			}

			// Process each experiment
			for _, experimentID := range experiments {
				log.Printf("Processing experiment %s for grading", experimentID)
				if err := service.ProcessExperiment(ctx, experimentID); err != nil {
					log.Printf("Failed to process experiment %s: %v", experimentID, err)
					// Continue with next experiment even if this one fails
				}
			}
		}
	}
}
