package server

import (
	"time"

	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
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

func newTestRouter() http.Handler {
	return New(Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Tokens: auth.NewTokens("secret", time.Hour),
	})
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	handler := newTestRouter()
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/teams"},
		{http.MethodGet, "/api/v1/teams"},
		{http.MethodPost, "/api/v1/teams/1/invite"},
		{http.MethodPatch, "/api/v1/teams/1/members/2"},
		{http.MethodDelete, "/api/v1/teams/1/members/2"},
	}
	for _, route := range routes {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d", route.method, route.path, rec.Code)
		}
	}
}
