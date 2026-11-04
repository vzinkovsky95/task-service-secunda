// Транзакция повторяется только при deadlock (1213), до трёх попыток: для MySQL
// это штатная практика, жертву выбирает сам InnoDB. Функция fn должна быть
// безопасна для повторного запуска, поэтому результат присваивается внутри неё.
// Каждый повтор пишется в лог, чтобы блокировки, которые ещё сталкиваются,
// не оставались незамеченными.

package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
)

const (
	duplicateEntry   = 1062
	deadlockFound    = 1213
	maxTxAttempts    = 3
	retryBackoffStep = 10 * time.Millisecond
)

type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func InTx(ctx context.Context, pool *sql.DB, fn func(tx *sql.Tx) error) error {
	for attempt := 1; ; attempt++ {
		err := runTx(ctx, pool, fn)
		if !IsDeadlock(err) || attempt == maxTxAttempts {
			return err
		}
		logx.From(ctx).Warn("transaction deadlock, retrying", "attempt", attempt)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * retryBackoffStep):
		}
	}
}

func runTx(ctx context.Context, pool *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func IsDuplicate(err error) bool {
	return mysqlErrorNumber(err) == duplicateEntry
}

func IsDeadlock(err error) bool {
	return mysqlErrorNumber(err) == deadlockFound
}

func mysqlErrorNumber(err error) uint16 {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number
	}
	return 0
}
