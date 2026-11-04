//go:build integration

// Контрактный тест: сценарий проходит через настоящий роутер с MySQL, а каждый
// запрос и каждый ответ проверяется по схемам из openapi.yaml. Если форма ответа
// или набор полей разойдутся со спецификацией, тест упадёт. В конце проверяется,
// что сценарий затронул все операции /api/v1 и /healthz.

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/vzinkovsky95/task-service-secunda/api"
	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
	"github.com/vzinkovsky95/task-service-secunda/internal/ratelimit"
	"github.com/vzinkovsky95/task-service-secunda/internal/server"
	"github.com/vzinkovsky95/task-service-secunda/internal/testdb"
)

var frozenClock = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 10, 0, time.UTC) }

type contract struct {
	t       *testing.T
	handler http.Handler
	router  routers.Router
	covered map[string]bool
}

func newContract(t *testing.T, authPerMinute int) *contract {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		t.Fatal(err)
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	client := cache.NewClient(miniredis.RunT(t).Addr(), cache.NewGuard(logger))
	handler := server.New(server.Deps{
		Logger:        logger,
		DB:            testdb.New(t),
		Tokens:        auth.NewTokens("contract-secret", time.Hour),
		Lists:         cache.NewTaskLists(client),
		Limiter:       ratelimit.NewLimiter(client, frozenClock),
		AuthPerMinute: authPerMinute,
		APIPerMinute:  100000,
	})
	return &contract{t: t, handler: handler, router: router, covered: map[string]bool{}}
}

type result struct {
	status int
	header http.Header
	body   map[string]any
}

func (c *contract) call(method, path, token string, payload any, wantStatus int) result {
	c.t.Helper()
	var raw []byte
	if payload != nil {
		var err error
		if raw, err = json.Marshal(payload); err != nil {
			c.t.Fatal(err)
		}
	}
	build := func() *http.Request {
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req
	}

	route, pathParams, err := c.router.FindRoute(build())
	if err != nil {
		c.t.Fatalf("%s %s: no such operation in openapi.yaml: %v", method, path, err)
	}

	ctx := context.Background()
	validation := &openapi3filter.RequestValidationInput{
		Request:    build(),
		PathParams: pathParams,
		Route:      route,
		Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	if wantStatus < 400 {
		if err := openapi3filter.ValidateRequest(ctx, validation); err != nil {
			c.t.Errorf("%s %s: the request does not match openapi.yaml: %v", method, path, err)
		}
	}

	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, build())
	if rec.Code != wantStatus {
		c.t.Fatalf("%s %s: status %d, want %d\n%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}

	response := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: validation,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	response.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(ctx, response); err != nil {
		c.t.Errorf("%s %s -> %d: the response does not match openapi.yaml: %v\n%s", method, path, rec.Code, err, rec.Body.String())
	}

	if wantStatus < 300 {
		c.covered[method+" "+route.Path] = true
	}

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return result{status: rec.Code, header: rec.Header(), body: body}
}

func (r result) errorCode() string {
	if inner, ok := r.body["error"].(map[string]any); ok {
		code, _ := inner["code"].(string)
		return code
	}
	return ""
}

func (r result) number(path ...string) float64 {
	var current any = r.body
	for _, key := range path {
		current = current.(map[string]any)[key]
	}
	return current.(float64)
}

func TestEveryResponseMatchesTheOpenAPISpec(t *testing.T) {
	c := newContract(t, 1000)

	c.call("GET", "/healthz", "", nil, 200)

	register := func(name string) {
		c.call("POST", "/api/v1/register", "", map[string]any{"email": name + "@contract.test", "password": "correct-horse", "name": name}, 201)
	}
	login := func(name string) string {
		res := c.call("POST", "/api/v1/login", "", map[string]any{"email": name + "@contract.test", "password": "correct-horse"}, 200)
		return res.body["token"].(string)
	}
	for _, name := range []string{"owner", "member", "plain", "outsider"} {
		register(name)
	}
	if code := c.call("POST", "/api/v1/register", "", map[string]any{"email": "owner@contract.test", "password": "correct-horse", "name": "x"}, 409).errorCode(); code != "email_taken" {
		t.Errorf("duplicate register: %s", code)
	}
	c.call("POST", "/api/v1/register", "", map[string]any{"email": "bad", "password": "short", "name": ""}, 400)
	c.call("POST", "/api/v1/login", "", map[string]any{"email": "owner@contract.test", "password": "wrong-password"}, 401)

	owner, member, plain, outsider := login("owner"), login("member"), login("plain"), login("outsider")
	if res := c.call("GET", "/api/v1/teams", "", nil, 401); res.header.Get("WWW-Authenticate") != "Bearer" {
		t.Error("401 without WWW-Authenticate")
	}

	team := c.call("POST", "/api/v1/teams", owner, map[string]any{"name": "Contract"}, 201)
	teamID := int(team.number("id"))
	c.call("POST", "/api/v1/teams", owner, map[string]any{"name": " "}, 400)
	c.call("GET", "/api/v1/teams", owner, nil, 200)

	invite := func(token, email, role string, want int) result {
		return c.call("POST", fmt.Sprintf("/api/v1/teams/%d/invite", teamID), token, map[string]any{"email": email + "@contract.test", "role": role}, want)
	}
	memberID := int(invite(owner, "member", "member", 201).number("user_id"))
	plainID := int(invite(owner, "plain", "member", 201).number("user_id"))
	invite(owner, "member", "member", 409)
	invite(owner, "nobody", "member", 404)
	invite(plain, "outsider", "member", 403)
	invite(outsider, "outsider", "member", 404)

	c.call("PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", teamID, memberID), owner, map[string]any{"role": "admin"}, 200)
	c.call("PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", teamID, memberID), plain, map[string]any{"role": "member"}, 403)
	c.call("PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", teamID, memberID), owner, map[string]any{"role": "owner"}, 400)

	var taskIDs []int
	for i := 1; i <= 3; i++ {
		res := c.call("POST", "/api/v1/tasks", owner, map[string]any{"team_id": teamID, "title": fmt.Sprintf("t%d", i), "assignee_id": memberID}, 201)
		taskIDs = append(taskIDs, int(res.number("id")))
	}
	c.call("POST", "/api/v1/tasks", owner, map[string]any{"team_id": teamID, "title": "x", "assignee_id": 999999}, 400)
	c.call("POST", "/api/v1/tasks", outsider, map[string]any{"team_id": teamID, "title": "x"}, 404)

	list := fmt.Sprintf("/api/v1/tasks?team_id=%d", teamID)
	page := c.call("GET", list+"&limit=2", owner, nil, 200)
	next, ok := page.body["next_cursor"].(float64)
	if !ok {
		t.Fatalf("first page of 2 out of 3 has next_cursor %v", page.body["next_cursor"])
	}
	last := c.call("GET", fmt.Sprintf("%s&limit=2&cursor=%d", list, int(next)), owner, nil, 200)
	if last.body["next_cursor"] != nil || last.number("total") != 1 {
		t.Errorf("last page: next_cursor %v, total %v (total after the cursor must be 1)", last.body["next_cursor"], last.body["total"])
	}
	c.call("GET", fmt.Sprintf("%s&cursor=%d&offset=1", list, int(next)), owner, nil, 400)
	c.call("GET", list+"&status=bogus", owner, nil, 400)
	c.call("GET", list, outsider, nil, 404)

	taskID := taskIDs[0]
	path := fmt.Sprintf("/api/v1/tasks/%d", taskID)
	update := func(token string, status string, title string, version int, want int) result {
		return c.call("PUT", path, token, map[string]any{"title": title, "description": "", "status": status, "assignee_id": memberID, "version": version}, want)
	}
	done := update(member, "done", "t1", 1, 200)
	if done.body["closed_at"] == nil || done.number("version") != 2 {
		t.Errorf("closing the task: %v", done.body)
	}
	stale := update(member, "in_progress", "t1", 1, 409)
	if stale.errorCode() != "version_conflict" || stale.number("error", "details", "current_version") != 2 {
		t.Errorf("409 body: %v", stale.body)
	}
	update(plain, "done", "renamed", 2, 403)
	c.call("PUT", path, owner, map[string]any{"title": "t1", "status": "done", "version": 2}, 400)
	c.call("PUT", "/api/v1/tasks/999999", owner, map[string]any{"title": "t", "description": "", "status": "todo", "assignee_id": nil, "version": 1}, 404)

	history := c.call("GET", path+"/history", plain, nil, 200)
	if history.number("total") != 2 {
		t.Errorf("history entries = %v, want 2 (create, close)", history.body["total"])
	}
	c.call("GET", path+"/history", outsider, nil, 404)
	c.call("GET", "/api/v1/tasks/abc/history", owner, nil, 400)
	c.call("POST", path+"/comments", plain, map[string]any{"content": "looks good"}, 201)
	c.call("POST", path+"/comments", plain, map[string]any{"content": ""}, 400)
	c.call("GET", path+"/comments", member, nil, 200)

	stats := fmt.Sprintf("/api/v1/teams/%d/stats", teamID)
	report := c.call("GET", stats, owner, nil, 200)
	if report.number("comments_total") != 1 || report.number("tasks_by_status", "done") != 1 {
		t.Errorf("report: %v", report.body)
	}
	c.call("GET", stats, plain, nil, 403)
	c.call("GET", stats, outsider, nil, 404)

	member2 := fmt.Sprintf("/api/v1/teams/%d/members/%d", teamID, plainID)
	c.call("DELETE", member2, owner, nil, 204)
	c.call("DELETE", member2, owner, nil, 404)
	c.call("DELETE", fmt.Sprintf("/api/v1/teams/%d/members/%d", teamID, memberID), member, nil, 403)

	var missing []string
	for _, operation := range allOperations(t) {
		if !c.covered[operation] {
			missing = append(missing, operation)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the scenario never exercised these operations successfully: %s", strings.Join(missing, ", "))
	}
}

func allOperations(t *testing.T) []string {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		t.Fatal(err)
	}
	var operations []string
	for path, item := range doc.Paths.Map() {
		if !strings.HasPrefix(path, "/api/v1/") && path != "/healthz" {
			continue
		}
		for method := range item.Operations() {
			operations = append(operations, method+" "+path)
		}
	}
	sort.Strings(operations)
	return operations
}

func TestRateLimitResponseMatchesTheOpenAPISpec(t *testing.T) {
	c := newContract(t, 1)
	credentials := map[string]any{"email": "nobody@contract.test", "password": "wrong-password"}

	c.call("POST", "/api/v1/login", "", credentials, 401)
	res := c.call("POST", "/api/v1/login", "", credentials, 429)

	if res.errorCode() != "rate_limited" || res.header.Get("Retry-After") == "" {
		t.Errorf("429: code %q, Retry-After %q", res.errorCode(), res.header.Get("Retry-After"))
	}
	if res.number("error", "details", "retry_after_seconds") < 1 {
		t.Errorf("429 details: %v", res.body)
	}
}

func TestServiceRoutesServeTheSpecAndTheUI(t *testing.T) {
	c := newContract(t, 1000)

	for path, wantType := range map[string]string{"/openapi.yaml": "application/yaml", "/docs": "text/html"} {
		rec := httptest.NewRecorder()
		c.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), wantType) {
			t.Errorf("%s: status %d, content type %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if rec.Body.String() != string(api.Spec) {
		t.Error("the served spec differs from the embedded one")
	}
}
