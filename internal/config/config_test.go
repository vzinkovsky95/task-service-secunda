package config

import (
	"log/slog"
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("MYSQL_HOST", "db")
	t.Setenv("MYSQL_DATABASE", "app")
	t.Setenv("MYSQL_USER", "user")
	t.Setenv("MYSQL_PASSWORD", "pass")
	t.Setenv("REDIS_ADDR", "redis:6379")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.MySQL.Addr != "db:3306" {
		t.Errorf("MySQL.Addr = %q", cfg.MySQL.Addr)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	for _, key := range []string{"JWT_SECRET", "MYSQL_HOST", "MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "REDIS_ADDR"} {
		t.Run(key, func(t *testing.T) {
			setRequired(t)
			t.Setenv(key, "")
			if _, err := Load(); err == nil {
				t.Fatalf("expected error without %s", key)
			}
		})
	}
}

func TestLoadRejectsBadLogLevel(t *testing.T) {
	setRequired(t)
	t.Setenv("LOG_LEVEL", "loud")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadJWTTTL(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil || cfg.JWTTTL != 24*time.Hour {
		t.Fatalf("default ttl = %v, err = %v", cfg.JWTTTL, err)
	}

	for _, bad := range []string{"soon", "0s", "-1h"} {
		t.Setenv("JWT_TTL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("JWT_TTL=%q accepted", bad)
		}
	}
}

func TestLoadRateLimitDefaultsAndProxies(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RateLimitAuthPerMinute != 10 || cfg.RateLimitAPIPerMinute != 120 || len(cfg.TrustedProxies) != 0 {
		t.Errorf("defaults = %d, %d, %v", cfg.RateLimitAuthPerMinute, cfg.RateLimitAPIPerMinute, cfg.TrustedProxies)
	}

	t.Setenv("RATE_LIMIT_AUTH_PER_MINUTE", "3")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.5 ,2001:db8::/32")
	cfg, err = Load()
	if err != nil || cfg.RateLimitAuthPerMinute != 3 || len(cfg.TrustedProxies) != 3 {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
	if got := cfg.TrustedProxies[1].String(); got != "192.168.1.5/32" {
		t.Errorf("single address became %s", got)
	}
}

func TestLoadRejectsBadRateLimitSettings(t *testing.T) {
	cases := map[string]string{
		"RATE_LIMIT_AUTH_PER_MINUTE": "0",
		"RATE_LIMIT_API_PER_MINUTE":  "many",
		"TRUSTED_PROXIES":            "10.0.0.0/8, nonsense",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			setRequired(t)
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q accepted", key, value)
			}
		})
	}
}
