// Логгер запроса передаётся через контекст: middleware кладёт логгер с id
// запроса, а сервисы, кеш и слой базы достают его, не зная про HTTP. Без
// логгера в контексте работает slog.Default().

package logx

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

func With(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
