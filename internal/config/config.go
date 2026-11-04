// Конфигурация читается только из окружения. Секретов и паролей по умолчанию нет:
// без JWT_SECRET и данных MySQL сервис не стартует.

package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

type Config struct {
	HTTPAddr  string
	LogLevel  slog.Level
	MySQL     *mysql.Config
	JWTSecret string
	JWTTTL    time.Duration
	RedisAddr string

	RateLimitAuthPerMinute int
	RateLimitAPIPerMinute  int
	TrustedProxies         []netip.Prefix
}

func Load() (Config, error) {
	var cfg Config

	cfg.HTTPAddr = getenv("HTTP_ADDR", ":8080")

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	required := map[string]string{}
	for _, key := range []string{"JWT_SECRET", "MYSQL_HOST", "MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "REDIS_ADDR"} {
		value := os.Getenv(key)
		if value == "" {
			return Config{}, fmt.Errorf("%s is required", key)
		}
		required[key] = value
	}

	cfg.JWTSecret = required["JWT_SECRET"]
	cfg.RedisAddr = required["REDIS_ADDR"]

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
	cfg.MySQL.Collation = db.Collation

	if cfg.RateLimitAuthPerMinute, err = positiveInt("RATE_LIMIT_AUTH_PER_MINUTE", 10); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitAPIPerMinute, err = positiveInt("RATE_LIMIT_API_PER_MINUTE", 120); err != nil {
		return Config{}, err
	}
	if cfg.TrustedProxies, err = parseProxies(os.Getenv("TRUSTED_PROXIES")); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func positiveInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func parseProxies(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is neither an IP nor a CIDR", item)
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
	}
	return prefixes, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
