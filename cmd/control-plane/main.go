package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
)

var (
	port         = flag.String("port", "50051", "gRPC server port")
	dbPath       = flag.String("db", "skillgate.db", "SQLite database path")
	casRoot      = flag.String("cas", "./cas", "CAS root directory")
	artifactsDir = flag.String("artifacts", "./artifacts", "Artifacts directory")
)

func main() {
	flag.Parse()

	// Override from environment
	if p := os.Getenv("PORT"); p != "" {
		*port = p
	}
	if db := os.Getenv("DB_PATH"); db != "" {
		*dbPath = db
	}
	if cas := os.Getenv("CAS_ROOT"); cas != "" {
		*casRoot = cas
	}
	if art := os.Getenv("ARTIFACTS_ROOT"); art != "" {
		*artifactsDir = art
	}

	// Initialize database
	// TODO: Setup SQLite database with schema

	// Create gRPC server
	grpcServer := grpc.NewServer()

	// TODO: Register services
	// pb.RegisterRunnerServiceServer(grpcServer, &runnerService{})

	// Listen
	lis, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("Control Plane listening on :%s", *port)
	log.Printf("Database: %s", *dbPath)
	log.Printf("CAS root: %s", *casRoot)
	log.Printf("Artifacts: %s", *artifactsDir)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		grpcServer.GracefulStop()
		cancel()
	}()

	// Start serving
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}

	<-ctx.Done()
	log.Println("Control Plane stopped")
}
