package scheduler

import (
	"testing"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
)

func TestLogicalAndAttemptIdentitiesRemainDistinct(t *testing.T) {
	pairID := "sha256:pair"
	logical, err := LogicalTrialID(pairID, "without_skill")
	if err != nil {
		t.Fatal(err)
	}
	attemptOne, err := identity.TrialID(pairID, "without_skill", 1)
	if err != nil {
		t.Fatal(err)
	}
	attemptTwo, err := identity.TrialID(pairID, "without_skill", 2)
	if err != nil {
		t.Fatal(err)
	}
	if logical == attemptOne || attemptOne == attemptTwo {
		t.Fatal("logical trial 与 attempt identity 必须独立")
	}
	again, _ := LogicalTrialID(pairID, "without_skill")
	if logical != again {
		t.Fatal("logical trial identity 必须稳定")
	}
}

func TestResultIdempotencyKeyIncludesAttemptAndManifest(t *testing.T) {
	first, _ := ResultIdempotencyKey("trial-1", "sha256:r1")
	same, _ := ResultIdempotencyKey("trial-1", "sha256:r1")
	different, _ := ResultIdempotencyKey("trial-1", "sha256:r2")
	if first != same || first == different {
		t.Fatal("result idempotency key 投影错误")
	}
}
