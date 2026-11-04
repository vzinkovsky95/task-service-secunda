package logx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestFromReturnsLoggerStoredInContext(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil)).With("request_id", "r-1")

	From(With(context.Background(), logger)).Info("hello")

	if !strings.Contains(out.String(), `"request_id":"r-1"`) {
		t.Errorf("log line = %s", out.String())
	}
}

func TestFromFallsBackToDefault(t *testing.T) {
	if From(context.Background()) != slog.Default() {
		t.Error("expected slog.Default() without a logger in context")
	}
}

func TestFromSurvivesWithoutCancel(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	ctx, cancel := context.WithCancel(With(context.Background(), logger))
	cancel()

	if From(context.WithoutCancel(ctx)) != logger {
		t.Error("logger lost after WithoutCancel")
	}
}
