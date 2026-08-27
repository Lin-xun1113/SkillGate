package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/grader"
	"github.com/Lin-xun1113/SkillGate/internal/grading"
	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
)

func serveCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	grpcAddr := flagValue(args, "--grpc-addr", ":50051")
	artifactsDir := flagValue(args, "--artifacts-dir", "./artifacts")
	gradersDir := flagValue(args, "--graders-dir", "./graders")
	pollIntervalStr := flagValue(args, "--grading-poll-interval", "5s")

	pollInterval, err := time.ParseDuration(pollIntervalStr)
	if err != nil {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "grading-poll-interval", fmt.Sprintf("invalid duration: %v", err))
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

	// Create runner server
	server := runner.NewServer(pgStore, nil)

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

	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.Serve(lis)
	}()

	if jsonOutput {
		printSuccess(true, map[string]any{
			"started":             true,
			"grpc_addr":           lis.Addr().String(),
			"database":            "connected",
			"grading_poller":      "started",
			"poll_interval":       pollInterval.String(),
			"artifacts_dir":       artifactsDir,
			"graders_dir":         gradersDir,
		})
	} else {
		fmt.Printf("skillgate runner server listening on %s\n", lis.Addr().String())
		fmt.Printf("grading poller started (interval: %v)\n", pollInterval)
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
