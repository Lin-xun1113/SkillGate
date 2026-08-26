package releasegate

import "context"

// Store persists frozen snapshots and release decisions.
type Store interface {
	SaveSnapshot(ctx context.Context, snap Snapshot) error
	GetSnapshot(ctx context.Context, experimentID string) (*Snapshot, error)
	SaveDecision(ctx context.Context, decision Decision) error
	GetDecision(ctx context.Context, experimentID string) (*Decision, error)
}
