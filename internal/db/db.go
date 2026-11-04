// Collation соединения совпадает с умолчанием схемы MySQL 8: при расхождении
// выражения над колонками таблиц в UNION падают с «Illegal mix of collations».

package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

const Collation = "utf8mb4_0900_ai_ci"

func Open(ctx context.Context, cfg *mysql.Config) (*sql.DB, error) {
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}

	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(20)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return pool, nil
}
