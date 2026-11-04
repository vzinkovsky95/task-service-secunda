package task

import (
	"slices"
	"strings"
	"testing"
)

func ptr(id uint64) *uint64 { return &id }

func baseTask() Task {
	return Task{Title: "T", Description: "D", Status: StatusTodo, AssigneeID: ptr(5), Version: 3}
}

func sameInput(t Task) UpdateInput {
	return UpdateInput{Title: t.Title, Description: t.Description, Status: t.Status, AssigneeID: t.AssigneeID, Version: t.Version}
}

func TestDiffNoChange(t *testing.T) {
	task := baseTask()
	if changes := diff(task, sameInput(task)); len(changes) != 0 {
		t.Errorf("changes = %v", changes)
	}
}

func TestDiffDetectsEachField(t *testing.T) {
	task := baseTask()
	cases := map[Field]func(*UpdateInput){
		FieldTitle:       func(in *UpdateInput) { in.Title = "other" },
		FieldDescription: func(in *UpdateInput) { in.Description = "other" },
		FieldStatus:      func(in *UpdateInput) { in.Status = StatusDone },
		FieldAssignee:    func(in *UpdateInput) { in.AssigneeID = ptr(6) },
	}
	for field, mutate := range cases {
		in := sameInput(task)
		mutate(&in)
		changes := diff(task, in)
		if got := changes.fields(); !slices.Equal(got, []Field{field}) {
			t.Errorf("%s: changed fields = %v", field, got)
		}
	}
}

func TestDiffAssigneeNullTransitions(t *testing.T) {
	task := baseTask()
	in := sameInput(task)
	in.AssigneeID = nil
	change := diff(task, in)[FieldAssignee]
	if change.Old != uint64(5) || change.New != nil {
		t.Errorf("unassign = %+v", change)
	}

	task.AssigneeID = nil
	in.AssigneeID = ptr(7)
	change = diff(task, in)[FieldAssignee]
	if change.Old != nil || change.New != uint64(7) {
		t.Errorf("assign = %+v", change)
	}

	in.AssigneeID = nil
	if changes := diff(task, in); len(changes) != 0 {
		t.Errorf("nil to nil reported as change: %v", changes)
	}
}

func TestNormalizeUpdate(t *testing.T) {
	valid := UpdateInput{Title: "  Title ", Status: StatusTodo, Version: 1}
	got, err := normalizeUpdate(valid)
	if err != nil || got.Title != "Title" {
		t.Fatalf("got %+v, %v", got, err)
	}

	bad := map[string]UpdateInput{
		"blank title":    {Title: "  ", Status: StatusTodo, Version: 1},
		"long title":     {Title: strings.Repeat("a", 256), Status: StatusTodo, Version: 1},
		"unknown status": {Title: "T", Status: "blocked", Version: 1},
		"zero version":   {Title: "T", Status: StatusTodo},
		"zero assignee":  {Title: "T", Status: StatusTodo, Version: 1, AssigneeID: ptr(0)},
		"long desc":      {Title: "T", Status: StatusTodo, Version: 1, Description: strings.Repeat("я", 10001)},
	}
	for name, in := range bad {
		if _, err := normalizeUpdate(in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestNormalizeCreate(t *testing.T) {
	if _, err := normalizeCreate(CreateInput{TeamID: 1, Title: "T"}); err != nil {
		t.Errorf("valid rejected: %v", err)
	}
	if _, err := normalizeCreate(CreateInput{Title: "T"}); err == nil {
		t.Error("missing team accepted")
	}
	if _, err := normalizeCreate(CreateInput{TeamID: 1}); err == nil {
		t.Error("missing title accepted")
	}
}

func TestNormalizeComment(t *testing.T) {
	if got, err := normalizeComment("  hi "); err != nil || got != "hi" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := normalizeComment("   "); err == nil {
		t.Error("blank accepted")
	}
	if _, err := normalizeComment(strings.Repeat("я", 5001)); err == nil {
		t.Error("long accepted")
	}
}

func TestCacheKeyDistinguishesEveryFilterAndCursor(t *testing.T) {
	assignee := uint64(5)
	cursor10, cursor20 := uint64(10), uint64(20)
	filters := map[string]ListFilter{
		"plain":           {TeamID: 1, Limit: 20},
		"status":          {TeamID: 1, Limit: 20, Status: StatusDone},
		"assignee":        {TeamID: 1, Limit: 20, AssigneeID: &assignee},
		"cursor 10":       {TeamID: 1, Limit: 20, Cursor: &cursor10},
		"cursor 20":       {TeamID: 1, Limit: 20, Cursor: &cursor20},
		"cursor and page": {TeamID: 1, Limit: 20, Cursor: &cursor10, Offset: 5},
		"limit":           {TeamID: 1, Limit: 10},
		"offset":          {TeamID: 1, Limit: 20, Offset: 5},
	}

	seen := map[string]string{}
	for name, filter := range filters {
		key := filter.cacheKey()
		if other, clash := seen[key]; clash {
			t.Errorf("%q and %q share the cache key %q", name, other, key)
		}
		seen[key] = name
	}
}
