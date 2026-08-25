package retry

import (
	"fmt"
	"math/rand/v2"
	"time"
)

type Category string

const (
	ProviderTransient    Category = "PROVIDER_TRANSIENT"
	DependencyTransient  Category = "DEPENDENCY_TRANSIENT"
	WorkerLost           Category = "WORKER_LOST"
	LeaseTimeout         Category = "LEASE_TIMEOUT"
	InvalidRequest       Category = "INVALID_REQUEST"
	ProtocolInvalid      Category = "PROTOCOL_INVALID"
	PolicyDenied         Category = "POLICY_DENIED"
	UnauthorizedRequest  Category = "UNAUTHORIZED_REQUEST"
	DeterministicFailure Category = "DETERMINISTIC_FAILURE"
	BudgetExhausted      Category = "BUDGET_EXHAUSTED"
)

var defaultRetryable = map[Category]struct{}{
	ProviderTransient: {}, DependencyTransient: {}, WorkerLost: {}, LeaseTimeout: {},
}

type Policy struct {
	MaxAttempts int
	Base        time.Duration
	Cap         time.Duration
	Retryable   map[Category]struct{}
}

func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts: 2,
		Base:        time.Second,
		Cap:         30 * time.Second,
		Retryable:   clone(defaultRetryable),
	}
}

func (p Policy) Validate() error {
	if p.MaxAttempts < 1 || p.MaxAttempts > 30 {
		return fmt.Errorf("max attempts 必须位于 1..30")
	}
	if p.Base <= 0 || p.Cap <= 0 || p.Base > p.Cap {
		return fmt.Errorf("backoff base/cap 不合法")
	}
	for category := range p.Retryable {
		if _, ok := defaultRetryable[category]; !ok {
			return fmt.Errorf("类别 %s 不允许重试", category)
		}
	}
	return nil
}

func (p Policy) Allows(category Category, currentAttempt int) bool {
	if currentAttempt < 1 || currentAttempt >= p.MaxAttempts {
		return false
	}
	_, ok := p.Retryable[category]
	return ok
}

// UpperBound 返回 Full Jitter 的封顶区间 [0, upper]。
func (p Policy) UpperBound(nextAttempt int) time.Duration {
	if nextAttempt <= 1 {
		return 0
	}
	upper := p.Base
	for i := 2; i < nextAttempt && upper < p.Cap; i++ {
		if upper > p.Cap/2 {
			upper = p.Cap
			break
		}
		upper *= 2
	}
	if upper > p.Cap {
		return p.Cap
	}
	return upper
}

func (p Policy) FullJitter(nextAttempt int, random func(time.Duration) time.Duration) time.Duration {
	upper := p.UpperBound(nextAttempt)
	if upper <= 0 {
		return 0
	}
	if random != nil {
		value := random(upper)
		if value < 0 {
			return 0
		}
		if value > upper {
			return upper
		}
		return value
	}
	return time.Duration(rand.Int64N(int64(upper) + 1))
}

func clone(input map[Category]struct{}) map[Category]struct{} {
	out := make(map[Category]struct{}, len(input))
	for key := range input {
		out[key] = struct{}{}
	}
	return out
}
