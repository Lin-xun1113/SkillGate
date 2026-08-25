package store

import (
	"context"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
)

// QueueStore 是 Scheduler 对持久化事实来源的最小边界。
// 实现不得让调用方绕过事务方法直接修改生命周期状态。
type QueueStore interface {
	Materialize(context.Context, *manifest.CompiledExperiment, scheduler.MaterializeOptions) (scheduler.MaterializeResult, error)
	Claim(context.Context, string, time.Duration) (scheduler.Claim, error)
	Start(context.Context, scheduler.Heartbeat) error
	Heartbeat(context.Context, scheduler.Heartbeat, time.Duration) error
	Complete(context.Context, scheduler.Completion) (scheduler.CommitResult, error)
	SweepExpired(context.Context, int) (scheduler.SweepResult, error)
	CancelExperiment(context.Context, string, string, string) (scheduler.CancelResult, error)
	Close()
}
