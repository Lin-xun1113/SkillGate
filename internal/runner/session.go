package runner

import (
	"fmt"
	"sync"
	"time"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	leasetoken "github.com/Lin-xun1113/SkillGate/internal/lease"
)

type WorkerSession struct {
	WorkerID        string
	SessionToken    string
	ProtocolVersion string
	WorkerVersion   string
	Capabilities    *runnerv1.WorkerCapabilities
	Environment     *runnerv1.WorkerEnvironment
	RegisteredAt    time.Time
	LastHeartbeatAt time.Time
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*WorkerSession
	ttl      time.Duration
}

var allowedCapabilities = map[string]map[string]struct{}{
	"harness": {"fixture": {}, "langgraph": {}},
	"grader":  {"deterministic": {}, "fixture": {}, "llm": {}},
	"sandbox": {"default": {}, "docker-restricted-v1": {}},
}

func NewSessionManager(ttl ...time.Duration) *SessionManager {
	sessionTTL := 5 * time.Minute
	if len(ttl) > 0 && ttl[0] > 0 {
		sessionTTL = ttl[0]
	}
	return &SessionManager{sessions: make(map[string]*WorkerSession), ttl: sessionTTL}
}

func (m *SessionManager) Register(req *runnerv1.RegisterWorkerRequest) (*WorkerSession, error) {
	if req.Capabilities == nil {
		req.Capabilities = &runnerv1.WorkerCapabilities{MaxConcurrency: 1}
	}
	if req.Capabilities.MaxConcurrency == 0 {
		req.Capabilities.MaxConcurrency = 1
	}
	if req.Capabilities.MaxConcurrency < 0 {
		return nil, fmt.Errorf("capabilities.max_concurrency must not be negative")
	}
	for _, value := range req.Capabilities.Harnesses {
		if _, ok := allowedCapabilities["harness"][value]; !ok {
			return nil, fmt.Errorf("unsupported harness capability: %s", value)
		}
	}
	for _, value := range req.Capabilities.Graders {
		if _, ok := allowedCapabilities["grader"][value]; !ok {
			return nil, fmt.Errorf("unsupported grader capability: %s", value)
		}
	}
	for _, value := range req.Capabilities.SandboxProfiles {
		if _, ok := allowedCapabilities["sandbox"][value]; !ok {
			return nil, fmt.Errorf("unsupported sandbox capability: %s", value)
		}
	}
	token, err := leasetoken.Generate()
	if err != nil {
		return nil, err
	}
	session := &WorkerSession{
		WorkerID:        req.WorkerId,
		SessionToken:    token.Plaintext,
		ProtocolVersion: req.ProtocolVersion,
		WorkerVersion:   req.WorkerVersion,
		Capabilities:    req.Capabilities,
		Environment:     req.Environment,
		RegisteredAt:    time.Now().UTC(),
		LastHeartbeatAt: time.Now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[req.WorkerId] = session
	return session, nil
}

func (m *SessionManager) Validate(workerID, sessionToken string) bool {
	if workerID == "" || sessionToken == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[workerID]
	if !ok || time.Since(sess.LastHeartbeatAt) > m.ttl {
		return false
	}
	return sess.SessionToken == sessionToken
}

func (m *SessionManager) TouchHeartbeat(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[workerID]; ok {
		sess.LastHeartbeatAt = time.Now().UTC()
	}
}
