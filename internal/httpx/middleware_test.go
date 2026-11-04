package httpx

import (
	"bytes"
	"encoding/json"
	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestHandler(logs *bytes.Buffer, inner http.HandlerFunc) http.Handler {
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return Chain(inner, WithRequestID, WithAccessLog(logger), WithRecover)
}

func TestRequestIDKeptWhenSafe(t *testing.T) {
	var logs bytes.Buffer
	handler := newTestHandler(&logs, func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Errorf("request id = %q", got)
	}
	if !strings.Contains(logs.String(), `"request_id":"abc-123"`) {
		t.Errorf("log has no request id: %s", logs.String())
	}
}

func TestRequestIDReplacedWhenUnsafe(t *testing.T) {
	var logs bytes.Buffer
	handler := newTestHandler(&logs, func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "bad id\nforged")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Request-Id")
	if got == "" || got == "bad id\nforged" {
		t.Errorf("request id = %q", got)
	}
}

func TestPanicBecomesJSONError(t *testing.T) {
	var logs bytes.Buffer
	handler := newTestHandler(&logs, func(w http.ResponseWriter, r *http.Request) { panic("boom") })

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal" || body.Error.RequestID != rec.Header().Get("X-Request-Id") {
		t.Errorf("body = %+v", body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("panic value leaked into response")
	}
}

func TestTooManyRequestsMapsTo429WithDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	WriteServiceError(rec, req, apperr.New(apperr.TooManyRequests, "rate_limited", "slow down").
		WithDetails(map[string]any{"retry_after_seconds": 7}))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "rate_limited" || body.Error.Details["retry_after_seconds"] != float64(7) {
		t.Errorf("body = %s", rec.Body.String())
	}
}
