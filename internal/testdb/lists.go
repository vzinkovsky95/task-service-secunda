//go:build integration

package testdb

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/vzinkovsky95/task-service-secunda/internal/breaker"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
)

func NewLists(t *testing.T) *cache.TaskLists {
	t.Helper()
	guard := breaker.New(breaker.Config{Threshold: 5, Cooldown: time.Second})
	return cache.NewTaskLists(cache.NewClient(miniredis.RunT(t).Addr(), guard))
}
