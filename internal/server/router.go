package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
	"github.com/vzinkovsky95/task-service-secunda/internal/user"
)

type Deps struct {
	Logger *slog.Logger
	DB     *sql.DB
	Tokens *auth.Tokens
	Lists  *cache.TaskLists
}

func New(deps Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health(deps.DB))

	authed := auth.Require(deps.Tokens)

	users, err := user.NewService(user.NewRepository(deps.DB), deps.Tokens)
	if err != nil {
		panic(err)
	}
	user.NewHandler(users).Routes(mux)

	teamRepo := team.NewRepository()
	taskRepo := task.NewRepository()

	teams := team.NewService(deps.DB, teamRepo, taskRepo, deps.Lists)
	team.NewHandler(teams).Routes(mux, authed)

	tasks := task.NewService(deps.DB, taskRepo, teamRepo, deps.Lists)
	task.NewHandler(tasks).Routes(mux, authed)

	return httpx.Chain(unmatched{mux},
		httpx.WithRequestID,
		httpx.WithAccessLog(deps.Logger),
		httpx.WithRecover,
	)
}

func health(pool *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.PingContext(ctx); err != nil {
			logx.From(r.Context()).Error("health check failed", "error", err)
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "unavailable", "database is unavailable")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

type unmatched struct {
	*http.ServeMux
}

func (m unmatched) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, pattern := m.Handler(r)
	if pattern != "" {
		m.ServeMux.ServeHTTP(w, r)
		return
	}

	probe := &probeWriter{header: http.Header{}}
	handler.ServeHTTP(probe, r)

	switch probe.status {
	case http.StatusNotFound:
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "route not found")
	case http.StatusMethodNotAllowed:
		w.Header().Set("Allow", probe.header.Get("Allow"))
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	default:
		handler.ServeHTTP(w, r)
	}
}

type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header { return p.header }

func (p *probeWriter) Write(data []byte) (int, error) { return len(data), nil }

func (p *probeWriter) WriteHeader(status int) { p.status = status }
