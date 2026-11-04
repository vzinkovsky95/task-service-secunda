//go:build integration

// Настоящий MySQL для интеграционных тестов: на каждый тест создаётся своя база
// с миграциями проекта, после теста она удаляется. Доступ через MYSQL_HOST и
// MYSQL_ROOT_PASSWORD; без них тест падает, а не пропускается молча.

package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

func New(t *testing.T) *sql.DB {
	t.Helper()

	host, password := os.Getenv("MYSQL_HOST"), os.Getenv("MYSQL_ROOT_PASSWORD")
	if host == "" || password == "" {
		t.Fatal("MYSQL_HOST and MYSQL_ROOT_PASSWORD are required for integration tests")
	}
	port := os.Getenv("MYSQL_PORT")
	if port == "" {
		port = "3306"
	}

	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = host + ":" + port
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Collation = db.Collation

	ctx := context.Background()
	admin, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	suffix := make([]byte, 5)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "it_" + hex.EncodeToString(suffix)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}

	cfg = cfg.Clone()
	cfg.DBName = name
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, cfg, discard); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)

	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE "+name); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
		admin.Close()
	})
	return pool
}
