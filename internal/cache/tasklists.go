// Кеш списков задач команды. Ключ: tasks:list:{team}:{поколение}:{фильтры}.
// У каждой команды есть счётчик поколения tasks:gen:{team}; любое изменение
// задач увеличивает его, и все прежние ключи команды становятся недостижимы
// (доедят TTL 5 минут).
//
// Три правила, от которых зависит корректность:
//   - поколение читается ДО похода в базу и фиксируется в Slot: запрос,
//     начавшийся раньше изменения, запишет свой результат под старый ключ,
//     который уже никто не прочитает;
//   - инвалидация вызывается только после коммита и не зависит от отмены
//     запроса клиентом, иначе разрыв соединения оставил бы устаревший кеш;
//   - у счётчиков нет TTL, а Redis работает с volatile-lru: вытеснять можно
//     только ключи с TTL. Иначе вытесненный счётчик вернулся бы к нулю и
//     открыл старые записи.
//
// Любая ошибка Redis считается промахом и пишется в лог: кеш не роняет запрос.
// Разомкнутый предохранитель отклоняет вызов сразу и без шума в логе: о самом
// размыкании сообщает предохранитель.
// Если инвалидацию выполнить не удалось, процесс запоминает команду и повторяет
// её перед чтением: иначе Redis, вернувшийся с прежними данными, отдал бы
// устаревший список. Отметка хранит номер неудачи и снимается только после
// успешного INCR и только если номер не изменился: пока INCR в полёте, другие
// читатели продолжают видеть отметку, а новая неудача не стирается. Параллельные
// INCR допустимы, они лишь двигают поколение дальше. Память о неудаче живёт до
// перезапуска процесса; дальше устаревшие записи ограничены TTL.

package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vzinkovsky95/task-service-secunda/internal/breaker"
	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
)

const (
	listTTL            = 5 * time.Minute
	invalidateAttempts = 2
	invalidateTimeout  = 600 * time.Millisecond
)

type TaskLists struct {
	client *redis.Client

	mu       sync.Mutex
	failures uint64
	pending  map[uint64]uint64
}

func NewTaskLists(client *redis.Client) *TaskLists {
	return &TaskLists{client: client, pending: map[uint64]uint64{}}
}

type Slot struct {
	key    string
	usable bool
}

func (c *TaskLists) Load(ctx context.Context, teamID uint64, filter string) ([]byte, Slot) {
	logger := logx.From(ctx)

	if !c.settlePending(ctx, teamID) {
		return nil, Slot{}
	}

	generation, err := c.client.Get(ctx, generationKey(teamID)).Uint64()
	if err != nil && !errors.Is(err, redis.Nil) {
		logUnavailable(ctx, "generation", err)
		return nil, Slot{}
	}

	slot := Slot{key: fmt.Sprintf("tasks:list:%d:%d:%s", teamID, generation, filter), usable: true}

	data, err := c.client.Get(ctx, slot.key).Bytes()
	switch {
	case err == nil:
		logger.Debug("task list cache hit", "team_id", teamID)
		return data, slot
	case errors.Is(err, redis.Nil):
		logger.Debug("task list cache miss", "team_id", teamID)
		return nil, slot
	default:
		logUnavailable(ctx, "get", err)
		return nil, Slot{}
	}
}

func (c *TaskLists) Store(ctx context.Context, slot Slot, data []byte) {
	if !slot.usable {
		return
	}
	if err := c.client.Set(ctx, slot.key, data, listTTL).Err(); err != nil {
		logUnavailable(ctx, "set", err)
	}
}

func (c *TaskLists) InvalidateTeam(ctx context.Context, teamID uint64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), invalidateTimeout)
	defer cancel()

	var err error
	for attempt := 0; attempt < invalidateAttempts; attempt++ {
		if err = c.client.Incr(ctx, generationKey(teamID)).Err(); err == nil {
			return
		}
		if errors.Is(err, breaker.ErrOpen) {
			break
		}
	}
	c.markPending(teamID)
	logx.From(ctx).Error("task list cache invalidation failed, will retry before the next read",
		"team_id", teamID, "error", err)
}

func (c *TaskLists) markPending(teamID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	c.pending[teamID] = c.failures
}

func (c *TaskLists) pendingFailure(teamID uint64) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	failure, ok := c.pending[teamID]
	return failure, ok
}

func (c *TaskLists) clearPending(teamID, failure uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending[teamID] == failure {
		delete(c.pending, teamID)
	}
}

func (c *TaskLists) settlePending(ctx context.Context, teamID uint64) bool {
	failure, isPending := c.pendingFailure(teamID)
	if !isPending {
		return true
	}

	if err := c.client.Incr(ctx, generationKey(teamID)).Err(); err != nil {
		logUnavailable(ctx, "pending invalidation", err)
		return false
	}
	c.clearPending(teamID, failure)
	return true
}

func logUnavailable(ctx context.Context, op string, err error) {
	if errors.Is(err, breaker.ErrOpen) {
		return
	}
	logx.From(ctx).Warn("task list cache unavailable", "op", op, "error", err)
}

func generationKey(teamID uint64) string {
	return fmt.Sprintf("tasks:gen:%d", teamID)
}
