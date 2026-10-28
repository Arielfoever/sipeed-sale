package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier 为 database / tx 之上的单行查询接口(pgx.Tx 与 pgxpool 均满足)。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// pgxRow 是单行查询结果(pgx.Row 的别名)。
type pgxRow = pgx.Row

// txDefaults 事务默认参数(读已提交即可:库存由 SELECT FOR UPDATE 保证)。
func txDefaults() pgx.TxOptions {
	return pgx.TxOptions{}
}

// rollback 静默回滚(defer 使用;已提交的事务回滚会返回 ErrTxClosed,忽略)。
func rollback(ctx context.Context, tx pgx.Tx) {
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		// 保留日志可见性:真正错误场景以 commit/上层错误为准
		_ = err
	}
}

// commit 提交事务。
func commit(ctx context.Context, tx pgx.Tx) error {
	return tx.Commit(ctx)
}

// isNoRows 判定为空(未找到)。
func isNoRows(err error) bool {
	return err != nil && errors.Is(err, pgx.ErrNoRows)
}

// isUniqueRequestID 判定 request_id 唯一键冲突(幂等路径)。
func isUniqueRequestID(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.ConstraintName == "orders_request_id_key"
	}
	return false
}
