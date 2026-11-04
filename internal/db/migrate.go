// Миграции только вперёд: файлы migrations/*.sql применяются по имени один раз,
// факт применения хранится в schema_migrations. Для файлов с несколькими
// выражениями открывается отдельное соединение с multiStatements, основной пул
// его не включает. Параллельный запуск сериализуется через GET_LOCK.

package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"

	"github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrateLockName = "tasksvc_migrate"

func Migrate(ctx context.Context, cfg *mysql.Config, logger *slog.Logger) error {
	migrationCfg := cfg.Clone()
	migrationCfg.MultiStatements = true

	connector, err := mysql.NewConnector(migrationCfg)
	if err != nil {
		return err
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()

	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect for migrations: %w", err)
	}
	defer conn.Close()

	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", migrateLockName).Scan(&locked); err != nil || locked != 1 {
		return fmt.Errorf("acquire migration lock: %v", err)
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", migrateLockName)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}
		script, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", name); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
		logger.Info("migration applied", "version", name)
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}
