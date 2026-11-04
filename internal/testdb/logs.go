//go:build integration

package testdb

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
)

const deadlockRetryMessage = "transaction deadlock"

type Logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *Logs) Context(ctx context.Context) context.Context {
	return logx.With(ctx, slog.New(slog.NewJSONHandler(l, nil)))
}

func (l *Logs) DeadlockRetries() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.buf.String(), deadlockRetryMessage)
}
