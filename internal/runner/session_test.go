package runner

import (
	"testing"
	"time"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
)

func TestSessionManagerTouchHeartbeatRefreshesIdleSession(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	session, err := manager.Register(&runnerv1.RegisterWorkerRequest{
		WorkerId:        "worker-1",
		ProtocolVersion: "runner.v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !manager.Validate(session.WorkerID, session.SessionToken) {
		t.Fatal("newly registered session should validate")
	}

	// Simulate a worker that has been idle past the TTL without relying on
	// wall-clock sleeps in the test.
	session.LastHeartbeatAt = time.Now().Add(-2 * manager.ttl)
	expiredAt := session.LastHeartbeatAt
	if manager.Validate(session.WorkerID, session.SessionToken) {
		t.Fatal("expired idle session should not validate")
	}
	manager.TouchHeartbeat(session.WorkerID)
	if !session.LastHeartbeatAt.After(expiredAt) {
		t.Fatal("TouchHeartbeat must refresh the session timestamp")
	}

	// The original timestamp is expired, while the refreshed one remains valid.
	// This models an idle worker continuing to poll for work.
	if !manager.Validate(session.WorkerID, session.SessionToken) {
		t.Fatal("refreshed idle session should remain valid")
	}
}
