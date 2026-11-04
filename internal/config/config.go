// Конфигурация читается только из окружения. Секретов и паролей по умолчанию нет:
// без JWT_SECRET и данных MySQL сервис не стартует.

package config

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Config struct {
	HTTPAddr  string
	LogLevel  slog.Level
	MySQL     *mysql.Config
	JWTSecret string
	JWTTTL    time.Duration
}

func Load() (Config, error) {
	var cfg Config

	cfg.HTTPAddr = getenv("HTTP_ADDR", ":8080")

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	required := map[string]string{}
	for _, key := range []string{"JWT_SECRET", "MYSQL_HOST", "MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD"} {
		value := os.Getenv(key)
		if value == "" {
			return Config{}, fmt.Errorf("%s is required", key)
		}
		required[key] = value
	}

	cfg.JWTSecret = required["JWT_SECRET"]

	ttl, err := time.ParseDuration(getenv("JWT_TTL", "24h"))
	if err != nil || ttl <= 0 {
		return Config{}, fmt.Errorf("JWT_TTL must be a positive duration")
	}
	cfg.JWTTTL = ttl

	cfg.MySQL = mysql.NewConfig()
	cfg.MySQL.User = required["MYSQL_USER"]
	cfg.MySQL.Passwd = required["MYSQL_PASSWORD"]
	cfg.MySQL.Net = "tcp"
	cfg.MySQL.Addr = required["MYSQL_HOST"] + ":" + getenv("MYSQL_PORT", "3306")
	cfg.MySQL.DBName = required["MYSQL_DATABASE"]
	cfg.MySQL.ParseTime = true
	cfg.MySQL.Loc = time.UTC
	cfg.MySQL.Collation = "utf8mb4_unicode_ci"

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
