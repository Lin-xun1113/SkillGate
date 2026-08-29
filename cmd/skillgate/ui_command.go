package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
	"github.com/Lin-xun1113/SkillGate/internal/ui"
)

func uiCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	listen := flagValue(args, "--listen", ":8080")
	artifactsDir := flagValue(args, "--artifacts-dir", os.Getenv("SKILLGATE_ARTIFACTS_DIR"))
	if artifactsDir == "" {
		artifactsDir = "./artifacts"
	}

	dbURL, err := databaseURL(args)
	if err != nil {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", "database", err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgStore, err := postgres.Open(ctx, dbURL)
	if err != nil {
		return returnWithError(jsonOutput, "DATABASE_UNAVAILABLE", "database", fmt.Sprintf("failed to connect: %v", err))
	}
	defer pgStore.Close()

	// Create UI server
	server := ui.NewServerWithArtifacts(pgStore, listen, artifactsDir)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start server in background
	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.ListenAndServe()
	}()

	if jsonOutput {
		printSuccess(true, map[string]any{
			"started":       true,
			"listen":        listen,
			"database":      "connected",
			"artifacts_dir": artifactsDir,
		})
	} else {
		fmt.Printf("Listening on %s\n", listen)
	}

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		if !jsonOutput {
			fmt.Printf("received signal %v, gracefully stopping server...\n", sig)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return returnWithError(jsonOutput, "IO_ERROR", "shutdown", fmt.Sprintf("shutdown failed: %v", err))
		}
		return 0
	case err := <-serverErrChan:
		return returnWithError(jsonOutput, "IO_ERROR", "server", fmt.Sprintf("server error: %v", err))
	}
}
