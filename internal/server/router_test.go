package server

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"time"

	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
	"github.com/vzinkovsky95/task-service-secunda/internal/ratelimit"
)

func TestUnknownRouteReturnsJSONError(t *testing.T) {
	handler := newTestRouter()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "not_found" || body.Error.RequestID == "" {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestWrongMethodReturnsJSON405(t *testing.T) {
	handler := newTestRouter()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Allow") == "" {
		t.Error("Allow header is empty")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
}

var frozenNow = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 10, 0, time.UTC) }

func newTestRouter() http.Handler {
	return newRouterWithLimits(cache.NewClient("127.0.0.1:1", cache.NewGuard(testLogger())), 10, 120)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testDepsWith(client *redis.Client, authPerMinute, apiPerMinute int) Deps {
	return Deps{
		Logger:        testLogger(),
		Tokens:        auth.NewTokens("secret", time.Hour),
		Lists:         cache.NewTaskLists(client),
		Limiter:       ratelimit.NewLimiter(client, frozenNow),
		AuthPerMinute: authPerMinute,
		APIPerMinute:  apiPerMinute,
	}
}

func testDeps() Deps {
	return testDepsWith(cache.NewClient("127.0.0.1:1", cache.NewGuard(testLogger())), 10, 120)
}

func newRouterWithLimits(client *redis.Client, authPerMinute, apiPerMinute int) http.Handler {
	return New(testDepsWith(client, authPerMinute, apiPerMinute))
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	handler := newTestRouter()
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/teams"},
		{http.MethodGet, "/api/v1/teams"},
		{http.MethodPost, "/api/v1/teams/1/invite"},
		{http.MethodPatch, "/api/v1/teams/1/members/2"},
		{http.MethodDelete, "/api/v1/teams/1/members/2"},
		{http.MethodPost, "/api/v1/tasks"},
		{http.MethodGet, "/api/v1/tasks"},
		{http.MethodPut, "/api/v1/tasks/1"},
		{http.MethodGet, "/api/v1/tasks/1/history"},
		{http.MethodGet, "/api/v1/tasks/1/comments"},
		{http.MethodPost, "/api/v1/tasks/1/comments"},
		{http.MethodGet, "/api/v1/teams/1/stats"},
	}
	for _, route := range routes {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d", route.method, route.path, rec.Code)
		}
	}
}

func TestRegisterAndLoginAreLimitedPerClientIPAndScope(t *testing.T) {
	client := cache.NewClient(miniredis.RunT(t).Addr(), cache.NewGuard(testLogger()))
	handler := newRouterWithLimits(client, 2, 120)

	post := func(path, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "9.9.9.9")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for i := 1; i <= 2; i++ {
		if rec := post("/api/v1/login", "203.0.113.7:1000"); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnauthorized {
			t.Fatalf("login %d: status %d", i, rec.Code)
		}
	}
	rec := post("/api/v1/login", "203.0.113.7:1000")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("3rd login: status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}

	if rec := post("/api/v1/register", "203.0.113.7:1000"); rec.Code == http.StatusTooManyRequests {
		t.Error("register shares the counter with login")
	}
	if rec := post("/api/v1/login", "203.0.113.8:1000"); rec.Code == http.StatusTooManyRequests {
		t.Error("another client IP is limited")
	}
	if rec := post("/api/v1/login", "203.0.113.7:2000"); rec.Code != http.StatusTooManyRequests {
		t.Error("a spoofed X-Forwarded-For or a new port reset the limit")
	}
}

func TestAuthenticatedRoutesAreLimitedPerUser(t *testing.T) {
	client := cache.NewClient(miniredis.RunT(t).Addr(), cache.NewGuard(testLogger()))
	handler := newRouterWithLimits(client, 10, 3)
	tokens := auth.NewTokens("secret", time.Hour)
	alice, _, _ := tokens.Issue(1)
	bob, _, _ := tokens.Issue(2)

	get := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 1; i <= 3; i++ {
		if code := get(alice); code != http.StatusBadRequest {
			t.Fatalf("request %d: status %d", i, code)
		}
	}
	if code := get(alice); code != http.StatusTooManyRequests {
		t.Errorf("4th request: status %d", code)
	}
	if code := get(bob); code != http.StatusBadRequest {
		t.Errorf("another user was limited: status %d", code)
	}
}

func TestRequestsWithoutValidTokenNeverReachTheUserLimiter(t *testing.T) {
	client := cache.NewClient(miniredis.RunT(t).Addr(), cache.NewGuard(testLogger()))
	handler := newRouterWithLimits(client, 10, 1)

	for range 5 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	}
}

func TestRateLimitIsSkippedWhenRedisIsDown(t *testing.T) {
	handler := newRouterWithLimits(cache.NewClient("127.0.0.1:1", cache.NewGuard(testLogger())), 1, 1)

	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader("{}"))
		req.RemoteAddr = "203.0.113.7:1000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d rejected while Redis is unavailable", i)
		}
	}
}
