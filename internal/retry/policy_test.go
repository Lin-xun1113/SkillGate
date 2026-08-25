package retry

import (
	"testing"
	"time"
)

func TestPolicyClassificationAndAttemptLimit(t *testing.T) {
	policy := DefaultPolicy()
	if !policy.Allows(WorkerLost, 1) {
		t.Fatal("worker lost 的第一次失败应可重试")
	}
	if policy.Allows(WorkerLost, 2) || policy.Allows(InvalidRequest, 1) {
		t.Fatal("达到 attempt 上限或不可重试类别必须拒绝")
	}
}

func TestFullJitterIsBoundedAndInjectable(t *testing.T) {
	policy := Policy{MaxAttempts: 10, Base: time.Second, Cap: 3 * time.Second, Retryable: map[Category]struct{}{WorkerLost: {}}}
	if got := policy.UpperBound(2); got != time.Second {
		t.Fatalf("attempt 2 upper=%s", got)
	}
	if got := policy.UpperBound(4); got != 3*time.Second {
		t.Fatalf("attempt 4 应受 cap 限制: %s", got)
	}
	if got := policy.FullJitter(3, func(upper time.Duration) time.Duration { return upper / 2 }); got != time.Second {
		t.Fatalf("可注入 jitter 错误: %s", got)
	}
}

func TestPolicyRejectsSecurityOrProtocolRetry(t *testing.T) {
	policy := DefaultPolicy()
	policy.Retryable[ProtocolInvalid] = struct{}{}
	if policy.Validate() == nil {
		t.Fatal("协议错误不得配置为可重试")
	}
}
