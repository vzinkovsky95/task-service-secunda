// Ограничение частоты: счётчик на фиксированное окно в Redis. Ключ содержит
// область (register, login, api), субъекта (IP или id пользователя) и номер
// окна. INCR и EXPIRE выполняются одним Lua-скриптом: если бы EXPIRE не дошёл
// до Redis, ключ без TTL, а при volatile-lru и невытесняемый, блокировал бы
// клиента навсегда. Фиксированное окно допускает до двойного лимита на стыке
// двух окон; для защиты от перебора пароля этого достаточно.
//
// Любая недоступность Redis, включая разомкнутый предохранитель, пропускает
// запрос: ограничитель не должен останавливать сервис.

package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vzinkovsky95/task-service-secunda/internal/breaker"
	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
)

var incrWithExpire = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

type Rule struct {
	Scope  string
	Limit  int
	Window time.Duration
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type Limiter struct {
	client *redis.Client
	now    func() time.Time
}

func NewLimiter(client *redis.Client, now func() time.Time) *Limiter {
	return &Limiter{client: client, now: now}
}

func (l *Limiter) Allow(ctx context.Context, rule Rule, subject string) Decision {
	now := l.now()
	windowStart := now.Truncate(rule.Window)
	key := fmt.Sprintf("rl:%s:%s:%d", rule.Scope, subject, windowStart.Unix())

	count, err := incrWithExpire.Run(ctx, l.client, []string{key}, int(rule.Window.Seconds())).Int64()
	if err != nil {
		if !errors.Is(err, breaker.ErrOpen) {
			logx.From(ctx).Warn("rate limiter unavailable, request allowed", "scope", rule.Scope, "error", err)
		}
		return Decision{Allowed: true}
	}

	if count > int64(rule.Limit) {
		return Decision{RetryAfter: windowStart.Add(rule.Window).Sub(now)}
	}
	return Decision{Allowed: true}
}
