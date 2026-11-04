package task

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
)

func decodeUpdate(t *testing.T, body string) (UpdateInput, error) {
	t.Helper()
	var req updateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	return req.toInput()
}

func TestUpdateRequestRequiresEveryField(t *testing.T) {
	full := `{"title":"T","description":"","status":"todo","assignee_id":null,"version":1}`
	input, err := decodeUpdate(t, full)
	if err != nil || input.AssigneeID != nil || input.Version != 1 {
		t.Fatalf("input = %+v, err = %v", input, err)
	}

	withAssignee := `{"title":"T","description":"","status":"todo","assignee_id":7,"version":1}`
	input, err = decodeUpdate(t, withAssignee)
	if err != nil || input.AssigneeID == nil || *input.AssigneeID != 7 {
		t.Fatalf("input = %+v, err = %v", input, err)
	}

	missingField := map[string]string{
		"title":       `{"description":"","status":"todo","assignee_id":null,"version":1}`,
		"description": `{"title":"T","status":"todo","assignee_id":null,"version":1}`,
		"status":      `{"title":"T","description":"","assignee_id":null,"version":1}`,
		"assignee_id": `{"title":"T","description":"","status":"todo","version":1}`,
		"version":     `{"title":"T","description":"","status":"todo","assignee_id":null}`,
	}
	for field, body := range missingField {
		if _, err := decodeUpdate(t, body); err == nil {
			t.Errorf("missing %s accepted", field)
		}
	}
}

func TestNextCursor(t *testing.T) {
	tasks := func(ids ...uint64) []Task {
		result := make([]Task, 0, len(ids))
		for _, id := range ids {
			result = append(result, Task{ID: id})
		}
		return result
	}
	cases := []struct {
		name   string
		tasks  []Task
		offset int
		total  int
		want   *uint64
	}{
		{"more tasks remain", tasks(9, 8, 7), 0, 10, ptr(7)},
		{"page ends exactly at the last task", tasks(9, 8, 7), 0, 3, nil},
		{"last short page", tasks(2, 1), 0, 2, nil},
		{"empty page", nil, 0, 0, nil},
		{"empty page past the end", nil, 30, 12, nil},
		{"offset page with more behind it", tasks(6, 5), 2, 10, ptr(5)},
		{"offset page that reaches the end", tasks(2, 1), 8, 10, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextCursor(tc.tasks, tc.offset, tc.total)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("nextCursor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskPageAlwaysCarriesNextCursorKeyOthersDoNot(t *testing.T) {
	params := httpx.PageParams{Limit: 20}

	withNothingLeft, _ := json.Marshal(taskPageResponse{Page: httpx.NewPage([]taskResponse{}, params, 0)})
	if !strings.Contains(string(withNothingLeft), `"next_cursor":null`) {
		t.Errorf("task page without a continuation: %s", withNothingLeft)
	}
	for _, key := range []string{`"items":`, `"limit":20`, `"offset":0`, `"total":0`} {
		if !strings.Contains(string(withNothingLeft), key) {
			t.Errorf("task page lost the field %s: %s", key, withNothingLeft)
		}
	}

	more, _ := json.Marshal(taskPageResponse{Page: httpx.NewPage([]taskResponse{}, params, 0), NextCursor: ptr(42)})
	if !strings.Contains(string(more), `"next_cursor":42`) {
		t.Errorf("task page with a continuation: %s", more)
	}

	history, _ := json.Marshal(httpx.NewPage([]historyResponse{}, params, 0))
	comments, _ := json.Marshal(httpx.NewPage([]commentResponse{}, params, 0))
	for name, body := range map[string][]byte{"history": history, "comments": comments} {
		if strings.Contains(string(body), "next_cursor") {
			t.Errorf("%s page has next_cursor: %s", name, body)
		}
	}
}

func TestListQueryCursorAndOffsetAreMutuallyExclusive(t *testing.T) {
	reject := []string{
		"/tasks?team_id=1&cursor=5&offset=10",
		"/tasks?team_id=1&cursor=5&offset=0",
		"/tasks?team_id=1&offset=0&cursor=5",
	}
	for _, target := range reject {
		_, _, err := parseListQuery(httptest.NewRequest("GET", target, nil))
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.Invalid || !strings.Contains(appErr.Message, "cursor and offset") {
			t.Errorf("%s: err = %v", target, err)
		}
	}

	accept := map[string]bool{
		"/tasks?team_id=1&cursor=5&limit=3":  true,
		"/tasks?team_id=1&offset=10&limit=3": true,
		"/tasks?team_id=1":                   true,
	}
	for target := range accept {
		if _, _, err := parseListQuery(httptest.NewRequest("GET", target, nil)); err != nil {
			t.Errorf("%s rejected: %v", target, err)
		}
	}
}
