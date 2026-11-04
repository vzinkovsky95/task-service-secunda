package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/api"
	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
	"github.com/vzinkovsky95/task-service-secunda/internal/ratelimit"
	"github.com/vzinkovsky95/task-service-secunda/internal/stats"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
	"github.com/vzinkovsky95/task-service-secunda/internal/user"
)

type Deps struct {
	Logger         *slog.Logger
	DB             *sql.DB
	Tokens         *auth.Tokens
	Lists          *cache.TaskLists
	Limiter        *ratelimit.Limiter
	TrustedProxies []netip.Prefix
	AuthPerMinute  int
	APIPerMinute   int
}

type routeTable struct {
	mux      *http.ServeMux
	patterns []string
}

func (t *routeTable) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	t.patterns = append(t.patterns, pattern)
	t.mux.HandleFunc(pattern, handler)
}

func New(deps Deps) http.Handler {
	handler, _ := build(deps)
	return handler
}

func build(deps Deps) (http.Handler, []string) {
	routes := &routeTable{mux: http.NewServeMux()}
	mux := routes
	mux.HandleFunc("GET /{$}", redirectToDocs)
	mux.HandleFunc("GET /healthz", health(deps.DB))
	mux.HandleFunc("GET /openapi.yaml", api.SpecHandler())
	mux.HandleFunc("GET /docs", api.UIHandler())

	perMinute := func(scope string, limit int) ratelimit.Rule {
		return ratelimit.Rule{Scope: scope, Limit: limit, Window: time.Minute}
	}
	byIP := ratelimit.ByClientIP(deps.TrustedProxies)
	byUser := func(r *http.Request) string { return strconv.FormatUint(auth.UserID(r.Context()), 10) }
	limitAPI := deps.Limiter.Middleware(perMinute("api", deps.APIPerMinute), byUser)
	requireToken := auth.Require(deps.Tokens)
	authed := func(next http.HandlerFunc) http.HandlerFunc { return requireToken(limitAPI(next)) }

	users := user.NewService(user.NewRepository(deps.DB), deps.Tokens)
	user.NewHandler(users).Routes(mux,
		deps.Limiter.Middleware(perMinute("register", deps.AuthPerMinute), byIP),
		deps.Limiter.Middleware(perMinute("login", deps.AuthPerMinute), byIP),
	)

	teamRepo := team.NewRepository()
	taskRepo := task.NewRepository()

	teams := team.NewService(deps.DB, teamRepo, taskRepo, deps.Lists)
	team.NewHandler(teams).Routes(mux, authed)

	tasks := task.NewService(deps.DB, taskRepo, teamRepo, deps.Lists)
	task.NewHandler(tasks).Routes(mux, authed)

	reports := stats.NewService(deps.DB, teamRepo, stats.NewRepository(), time.Now)
	stats.NewHandler(reports).Routes(mux, authed)

	handler := httpx.Chain(unmatched{routes.mux},
		httpx.WithRequestID,
		httpx.WithAccessLog(deps.Logger),
		httpx.WithRecover,
	)
	return handler, routes.patterns
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

func redirectToDocs(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/docs", http.StatusFound)
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
