package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/scheduler"
	storecontract "github.com/Lin-xun1113/SkillGate/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	pool   *pgxpool.Pool
	jitter func(time.Duration) time.Duration
}

var _ storecontract.QueueStore = (*Store)(nil)

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	if databaseURL == "" {
		return nil, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "database URL 不能为空"}
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, &scheduler.Error{Code: scheduler.CodeDatabaseUnavailable, Message: "数据库配置无效", Cause: err}
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, &scheduler.Error{Code: scheduler.CodeDatabaseUnavailable, Message: "无法创建数据库连接池", Cause: err}
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, &scheduler.Error{Code: scheduler.CodeDatabaseUnavailable, Message: "无法连接 PostgreSQL", Cause: err}
	}
	return &Store{pool: pool}, nil
}

func OpenSQL(databaseURL string) (*sql.DB, error) {
	if databaseURL == "" {
		return nil, &scheduler.Error{Code: scheduler.CodeInvalidArgument, Message: "database URL 不能为空"}
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, &scheduler.Error{Code: scheduler.CodeDatabaseUnavailable, Message: "无法创建 migration 数据库连接", Cause: err}
	}
	return db, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) WithJitter(random func(time.Duration) time.Duration) *Store {
	s.jitter = random
	return s
}

func wrapDatabaseError(operation string, err error) error {
	return &scheduler.Error{Code: scheduler.CodeDatabaseUnavailable, Message: fmt.Sprintf("%s失败", operation), Cause: err}
}
