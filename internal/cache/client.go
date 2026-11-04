// Один клиент и один предохранитель на весь Redis: кеш и rate limit делят
// состояние здоровья. Короткие таймауты и ноль повторов, в том числе при
// подключении (по умолчанию go-redis делает пять попыток с паузами, это около
// секунды на каждый запрос, пока предохранитель не разомкнулся): любая ошибка
// быстро превращается в промах, и запрос идёт в базу. Предохранитель считает сбоем
// только то, что говорит о недоступности: сетевые ошибки и таймауты. Ответ
// «нет ключа», ошибка команды от сервера и отмена запроса клиентом сбоем не
// считаются; отмена к тому же не доказывает здоровья, поэтому она нейтральна.
//
// Предохранитель оборачивает логическую команду один раз. При создании
// соединения go-redis сам выполняет служебные команды рукопожатия, и они
// проходят через тот же hook: без пометки в контексте они наталкивались бы на
// единственный слот пробы, и после рестарта Redis цепь не замыкалась бы никогда.

package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vzinkovsky95/task-service-secunda/internal/breaker"
	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
)

const (
	operationTimeout = 200 * time.Millisecond
	breakerThreshold = 5
	breakerCooldown  = 5 * time.Second
)

func NewGuard(logger *slog.Logger) *breaker.Breaker {
	return breaker.New(breaker.Config{Threshold: breakerThreshold, Cooldown: breakerCooldown, Logger: logger})
}

type structuredPrinter struct{}

func (structuredPrinter) Printf(ctx context.Context, format string, args ...any) {
	logx.From(ctx).Warn(fmt.Sprintf(format, args...), "component", "go-redis")
}

func UseStructuredLogs() {
	redis.SetLogger(structuredPrinter{})
}

func NewClient(addr string, guard *breaker.Breaker) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:          addr,
		DialTimeout:   operationTimeout,
		ReadTimeout:   operationTimeout,
		WriteTimeout:  operationTimeout,
		PoolTimeout:   operationTimeout,
		MaxRetries:    -1,
		DialerRetries: 1,
	})
	client.AddHook(breakerHook{guard: guard})
	return client
}

type breakerHook struct {
	guard *breaker.Breaker
}

func (h breakerHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

type guardedKey struct{}

func (h breakerHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if ctx.Value(guardedKey{}) != nil {
			return next(ctx, cmd)
		}
		done, err := h.guard.Allow()
		if err != nil {
			return err
		}
		err = next(context.WithValue(ctx, guardedKey{}, true), cmd)
		done(outcomeOf(err))
		return err
	}
}

func (h breakerHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if ctx.Value(guardedKey{}) != nil {
			return next(ctx, cmds)
		}
		done, err := h.guard.Allow()
		if err != nil {
			return err
		}
		err = next(context.WithValue(ctx, guardedKey{}, true), cmds)
		done(outcomeOf(err))
		return err
	}
}

func outcomeOf(err error) breaker.Outcome {
	var serverErr redis.Error
	switch {
	case err == nil, errors.Is(err, redis.Nil), errors.As(err, &serverErr):
		return breaker.Success
	case errors.Is(err, context.Canceled):
		return breaker.Ignore
	default:
		return breaker.Failure
	}
}
