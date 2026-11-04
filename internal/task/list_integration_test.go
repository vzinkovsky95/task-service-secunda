//go:build integration

package task_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/task"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
	"github.com/vzinkovsky95/task-service-secunda/internal/testdb"
)

var seededAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type listFixture struct {
	service *task.Service
	actor   uint64
	teamID  uint64
	ids     []uint64
}

func newListFixture(t *testing.T) listFixture {
	t.Helper()
	pool := testdb.New(t)
	seed := testdb.NewSeeder(t, pool)

	lists := testdb.NewLists(t)
	teamRepo := team.NewRepository()
	f := listFixture{service: task.NewService(pool, task.NewRepository(), teamRepo, lists)}

	f.actor = seed.User("actor")
	other := seed.User("other")
	f.teamID = seed.Team("T", f.actor)
	otherTeam := seed.Team("O", other)

	for i := 0; i < 12; i++ {
		status, assignee := "todo", &f.actor
		if i%3 == 0 {
			status = "done"
		}
		if i%4 == 0 {
			assignee = nil
		}
		f.ids = append(f.ids, seed.Task(testdb.Task{TeamID: f.teamID, CreatedBy: f.actor, AssigneeID: assignee, Status: status, CreatedAt: seededAt}))
	}
	seed.Task(testdb.Task{TeamID: otherTeam, CreatedBy: other, Status: "todo", CreatedAt: seededAt})
	return f
}

func (f listFixture) page(t *testing.T, filter task.ListFilter) ([]uint64, int) {
	t.Helper()
	filter.TeamID = f.teamID
	tasks, total, err := f.service.List(context.Background(), f.actor, filter)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint64, 0, len(tasks))
	for _, item := range tasks {
		ids = append(ids, item.ID)
	}
	return ids, total
}

func descending(ids []uint64) []uint64 {
	result := slices.Clone(ids)
	slices.Reverse(result)
	return result
}

func TestCursorWalksTheWholeListWithoutGapsOrRepeats(t *testing.T) {
	f := newListFixture(t)
	want := descending(f.ids)

	var walked []uint64
	var cursor *uint64
	for step := 0; step < 10; step++ {
		page, _ := f.page(t, task.ListFilter{Limit: 5, Cursor: cursor})
		if len(page) == 0 {
			break
		}
		walked = append(walked, page...)
		last := page[len(page)-1]
		cursor = &last
	}

	if !slices.Equal(walked, want) {
		t.Errorf("walked %v, want %v", walked, want)
	}
}

func TestCursorIsExclusiveAndTotalCountsTheRemainder(t *testing.T) {
	f := newListFixture(t)
	cursor := f.ids[6]

	page, total := f.page(t, task.ListFilter{Limit: 100, Cursor: &cursor})

	if want := descending(f.ids[:6]); !slices.Equal(page, want) {
		t.Errorf("page = %v, want %v", page, want)
	}
	if total != 6 {
		t.Errorf("total = %d, want 6 (tasks matching the filters after the cursor)", total)
	}
}

func TestCursorCombinesWithFilters(t *testing.T) {
	f := newListFixture(t)
	cursor := f.ids[11]

	done, _ := f.page(t, task.ListFilter{Limit: 100, Status: task.StatusDone, Cursor: &cursor})
	for _, id := range done {
		if id >= cursor {
			t.Errorf("id %d is not below the cursor %d", id, cursor)
		}
	}
	if want := []uint64{f.ids[9], f.ids[6], f.ids[3], f.ids[0]}; !slices.Equal(done, want) {
		t.Errorf("done below cursor = %v, want %v", done, want)
	}

	assigned := f.actor
	mine, _ := f.page(t, task.ListFilter{Limit: 100, Status: task.StatusDone, AssigneeID: &assigned, Cursor: &cursor})
	if want := []uint64{f.ids[9], f.ids[6], f.ids[3]}; !slices.Equal(mine, want) {
		t.Errorf("done and assigned below cursor = %v, want %v", mine, want)
	}
}

func TestSameQueryWithAndWithoutCursorNeverShareACacheEntry(t *testing.T) {
	f := newListFixture(t)
	cursorA, cursorB := f.ids[8], f.ids[4]

	for round := 0; round < 2; round++ {
		all, _ := f.page(t, task.ListFilter{Limit: 100})
		a, _ := f.page(t, task.ListFilter{Limit: 100, Cursor: &cursorA})
		b, _ := f.page(t, task.ListFilter{Limit: 100, Cursor: &cursorB})

		if len(all) != 12 || len(a) != 8 || len(b) != 4 {
			t.Fatalf("round %d (second one is served from the cache): sizes %d, %d, %d", round+1, len(all), len(a), len(b))
		}
	}
}

func TestListStillAnswersWithoutCursor(t *testing.T) {
	f := newListFixture(t)

	page, total := f.page(t, task.ListFilter{Limit: 5, Offset: 2})

	if want := descending(f.ids)[2:7]; !slices.Equal(page, want) {
		t.Errorf("page = %v, want %v", page, want)
	}
	if total != 12 {
		t.Errorf("total = %d, want 12", total)
	}
}
