package runner

import (
	"fmt"
	"net"
	"sync"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	"github.com/Lin-xun1113/SkillGate/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	grpcServer *grpc.Server
	service    *RunnerService
	listener   net.Listener
	mu         sync.Mutex
	running    bool
}

func NewServer(qStore store.QueueStore, opts *ServiceOptions, grpcOpts ...grpc.ServerOption) *Server {
	svc := NewRunnerService(qStore, opts)
	gServer := grpc.NewServer(grpcOpts...)
	runnerv1.RegisterRunnerControlServer(gServer, svc)
	reflection.Register(gServer)

	return &Server{
		grpcServer: gServer,
		service:    svc,
	}
}

func (s *Server) Service() *RunnerService {
	return s.service
}

func (s *Server) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = lis
	s.running = true
	s.mu.Unlock()

	return s.grpcServer.Serve(lis)
}

func (s *Server) Serve(lis net.Listener) error {
	s.mu.Lock()
	s.listener = lis
	s.running = true
	s.mu.Unlock()

	return s.grpcServer.Serve(lis)
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.grpcServer.GracefulStop()
	s.running = false
}

func (s *Server) ForceStop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.grpcServer.Stop()
	s.running = false
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}
