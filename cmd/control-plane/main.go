package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/grader"
	"github.com/Lin-xun1113/SkillGate/internal/grading"
	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
	"google.golang.org/grpc"
)

var (
	port              = flag.String("port", "50051", "gRPC server port")
	dbPath            = flag.String("db", "skillgate.db", "SQLite database path")
	casRoot           = flag.String("cas", "./cas", "CAS root directory")
	artifactsDir      = flag.String("artifacts", "./artifacts", "Artifacts directory")
	gradersDir        = flag.String("graders", "./graders", "Graders directory")
	gradingPollInterval = flag.Duration("grading-poll-interval", 5*time.Second, "Grading poll interval")
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
	if grd := os.Getenv("GRADERS_DIR"); grd != "" {
		*gradersDir = grd
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize database (PostgreSQL)
	// TODO: Read connection string from environment
	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		log.Println("DATABASE_URL not set, using default")
		connString = "postgres://localhost:5432/skillgate?sslmode=disable"
	}

	store, err := postgres.Open(ctx, connString)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer store.Close()

	// Initialize Grader Registry
	graderRegistry := grader.NewRegistry(*gradersDir)
	if err := graderRegistry.LoadFromDirectory(*gradersDir); err != nil {
		log.Printf("Warning: failed to load graders from %s: %v", *gradersDir, err)
	}

	// Initialize Grading Service
	gradingService := grading.NewService(store, graderRegistry, *artifactsDir, *gradingPollInterval)

	// Start Grading Poller
	go startGradingPoller(ctx, gradingService, store, *gradingPollInterval)

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
	log.Printf("Graders: %s", *gradersDir)
	log.Printf("Grading poll interval: %v", *gradingPollInterval)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		grpcServer.GracefulStop()
		gradingService.Stop()
		cancel()
	}()

	// Start serving
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}

	<-ctx.Done()
	log.Println("Control Plane stopped")
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
