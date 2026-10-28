// Package store 是全项目唯一允许出现 SQL 的包(pgx + 手写 SQL),
// 所有 PostgreSQL 访问集中于此,便于未来更换存储或迁移。
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store 封装所有 PostgreSQL 访问。
type Store struct {
	pool *pgxpool.Pool
}

// ErrNoMigrationsNeeded 由 migrate 返回,表示 schema 已是最新。
var ErrNoMigrationsNeeded = migrate.ErrNoChange

// New 创建连接池;autoMigrate 为 true 时同步执行 golang-migrate 迁移
// (迁移文件以 ioFS 内嵌,部署侧无需安装 migrate CLI)。
func New(ctx context.Context, databaseURL string, autoMigrate bool) (*Store, error) {
	// 合理的池上限;生产上再调
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 8
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	s := &Store{pool: pool}
	if autoMigrate {
		if err := s.Migrate(databaseURL); err != nil && !errors.Is(err, ErrNoMigrationsNeeded) {
			pool.Close()
			return nil, err
		}
	}
	return s, nil
}

// Close 关闭连接池。
func (s *Store) Close() {
	s.pool.Close()
}

// Migrate 使用 golang-migrate + ioFS 将 database schema 迁移到最新。
// 所有纯 SQL 文件都提交在 migrations/ 目录,版本化、可回滚。
func (s *Store) Migrate(databaseURL string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		// 关闭 migrate 的 source 与 database 连接;错误不影响主流程
		_, _ = m.Close()
	}()
	if err := m.Up(); err != nil {
		// migrate 内部会把底层错误包装在 *database.Error 里,这里简化透传
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
