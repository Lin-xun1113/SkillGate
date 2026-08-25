package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/runner"
	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
)

func serveCommand(args []string) int {
	jsonOutput := contains(args, "--json")
	grpcAddr := flagValue(args, "--grpc-addr", ":50051")
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

	server := runner.NewServer(pgStore, nil)

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", grpcAddr, fmt.Sprintf("failed to listen on %s: %v", grpcAddr, err))
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.Serve(lis)
	}()

	if jsonOutput {
		printSuccess(true, map[string]any{
			"started":   true,
			"grpc_addr": lis.Addr().String(),
			"database":  "connected",
		})
	} else {
		fmt.Printf("skillgate runner server listening on %s\n", lis.Addr().String())
	}

	select {
	case sig := <-sigChan:
		if !jsonOutput {
			fmt.Printf("received signal %v, gracefully stopping server...\n", sig)
		}
		server.Stop()
		return 0
	case err := <-serverErrChan:
		if err != nil {
			return returnWithError(jsonOutput, "IO_ERROR", grpcAddr, fmt.Sprintf("server error: %v", err))
		}
		return 0
	}
}
