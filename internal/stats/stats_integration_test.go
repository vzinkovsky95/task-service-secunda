//go:build integration

package stats

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
	"github.com/vzinkovsky95/task-service-secunda/internal/testdb"
)

var now = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	service *Service
	teamA   uint64
	teamB   uint64
	teamC   uint64
	owner   uint64
	admin   uint64
	member  uint64
	outside uint64
	users   [8]uint64
	avgSecs float64
}

func buildFixture(t *testing.T) fixture {
	t.Helper()
	pool := testdb.New(t)
	seed := testdb.NewSeeder(t, pool)

	var f fixture
	f.service = NewService(pool, team.NewRepository(), NewRepository(), func() time.Time { return now })

	names := []string{"", "Zoe", "Adam", "Mia", "Bob", "Eve", "Kim", "Lee"}
	for i := 1; i <= 7; i++ {
		f.users[i] = seed.User(names[i])
	}
	f.owner = seed.User("Owner A")
	f.admin = seed.User("Admin A")
	f.member = seed.User("Member A")
	f.outside = seed.User("Outsider")
	ownerB := seed.User("Owner B")
	ownerC := seed.User("Owner C")

	f.teamA = seed.Team("A", f.owner)
	f.teamB = seed.Team("B", ownerB)
	f.teamC = seed.Team("C", ownerC)
	seed.Member(f.teamA, f.admin, "admin")
	seed.Member(f.teamA, f.member, "member")
	for i := 1; i <= 6; i++ {
		seed.Member(f.teamA, f.users[i], "member")
	}
	seed.Member(f.teamB, f.users[1], "member")
	seed.Member(f.teamB, f.users[7], "member")

	since := now.Add(-window)
	var closedDurations []time.Duration

	openTask := func(teamID uint64, status string, assignee *uint64) uint64 {
		return seed.Task(testdb.Task{TeamID: teamID, CreatedBy: f.owner, AssigneeID: assignee, Status: status, CreatedAt: now.Add(-48 * time.Hour)})
	}
	closedTask := func(teamID uint64, assignee *uint64, closedAt time.Time, duration time.Duration) uint64 {
		createdAt := closedAt.Add(-duration)
		if teamID == f.teamA {
			closedDurations = append(closedDurations, duration)
		}
		return seed.Task(testdb.Task{TeamID: teamID, CreatedBy: f.owner, AssigneeID: assignee, Status: "done", CreatedAt: createdAt, ClosedAt: &closedAt})
	}
	user := func(i int) *uint64 { return &f.users[i] }

	openTask(f.teamA, "todo", user(1))
	openTask(f.teamA, "todo", nil)
	commentedOpen := openTask(f.teamA, "in_progress", user(2))
	seed.Comments(commentedOpen, f.users[2], 2)

	u1Tasks := []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour}
	var firstU1Task uint64
	for i, d := range u1Tasks {
		id := closedTask(f.teamA, user(1), now.Add(-time.Duration(i+1)*24*time.Hour), d)
		if i == 0 {
			firstU1Task = id
		}
	}
	seed.Comments(firstU1Task, f.users[1], 3)

	for i, d := range []time.Duration{time.Hour, time.Hour, 4 * time.Hour} {
		closedTask(f.teamA, user(2), now.Add(-time.Duration(i+3)*24*time.Hour), d)
	}
	closedTask(f.teamA, user(3), now.Add(-5*24*time.Hour), 2*time.Hour)
	closedTask(f.teamA, user(3), since, 2*time.Hour)
	closedTask(f.teamA, user(4), now.Add(-5*24*time.Hour), 6*time.Hour)
	closedTask(f.teamA, user(4), now.Add(-6*24*time.Hour), 6*time.Hour)
	closedTask(f.teamA, user(4), since.Add(-time.Second), 6*time.Hour)
	for i := 0; i < 5; i++ {
		closedTask(f.teamA, user(5), now.Add(-time.Duration(31+i)*24*time.Hour), time.Hour)
	}
	closedTask(f.teamA, nil, now.Add(-2*24*time.Hour), 10*time.Hour)

	for i := 0; i < 10; i++ {
		id := closedTask(f.teamB, user(7), now.Add(-24*time.Hour), time.Hour)
		seed.Comments(id, f.users[7], 1)
	}
	for i := 0; i < 4; i++ {
		closedTask(f.teamB, user(1), now.Add(-24*time.Hour), time.Hour)
	}
	openTask(f.teamB, "todo", user(7))
	seed.Comments(openTask(f.teamB, "in_progress", nil), f.users[7], 3)

	var sum time.Duration
	for _, d := range closedDurations {
		sum += d
	}
	f.avgSecs = sum.Seconds() / float64(len(closedDurations))
	return f
}

func TestReportCountsOnlyOneTeamAndRespectsWindow(t *testing.T) {
	f := buildFixture(t)

	report, err := f.service.Get(context.Background(), f.owner, f.teamA)
	if err != nil {
		t.Fatal(err)
	}

	wantStatuses := map[task.Status]int{task.StatusTodo: 2, task.StatusInProgress: 1, task.StatusDone: 17}
	for status, want := range wantStatuses {
		if got := report.TasksByStatus[status]; got != want {
			t.Errorf("tasks in %s = %d, want %d", status, got, want)
		}
	}

	wantTop := []Assignee{
		{UserID: f.users[1], Name: "Zoe", ClosedTasks: 3},
		{UserID: f.users[2], Name: "Adam", ClosedTasks: 3},
		{UserID: f.users[3], Name: "Mia", ClosedTasks: 2},
	}
	if len(report.TopAssignees) != len(wantTop) {
		t.Fatalf("top = %+v", report.TopAssignees)
	}
	for i, want := range wantTop {
		if report.TopAssignees[i] != want {
			t.Errorf("top[%d] = %+v, want %+v", i, report.TopAssignees[i], want)
		}
	}

	if math.Abs(report.AvgCloseSeconds-f.avgSecs) > 0.001 {
		t.Errorf("avg close seconds = %v, want %v", report.AvgCloseSeconds, f.avgSecs)
	}
	if report.CommentsTotal != 5 {
		t.Errorf("comments = %d, want 5", report.CommentsTotal)
	}
}

func TestReportWindowMovesWithNow(t *testing.T) {
	f := buildFixture(t)

	laterNow := now.Add(time.Second)
	service := NewService(f.service.pool, f.service.members, f.service.repo, func() time.Time { return laterNow })
	report, err := service.Get(context.Background(), f.owner, f.teamA)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.TopAssignees) != 3 || report.TopAssignees[2].UserID != f.users[4] {
		t.Fatalf("after the window moved past the boundary task the third place must go to Bob: %+v", report.TopAssignees)
	}
}

func TestReportOfEmptyTeamIsZerosAndEmptyLists(t *testing.T) {
	f := buildFixture(t)
	var ownerC uint64
	if err := f.service.pool.QueryRow("SELECT created_by FROM teams WHERE id = ?", f.teamC).Scan(&ownerC); err != nil {
		t.Fatal(err)
	}

	report, err := f.service.Get(context.Background(), ownerC, f.teamC)
	if err != nil {
		t.Fatal(err)
	}

	if report.TopAssignees == nil || len(report.TopAssignees) != 0 {
		t.Errorf("top assignees = %#v, want an empty non-nil slice", report.TopAssignees)
	}
	for _, status := range task.Statuses() {
		if count, ok := report.TasksByStatus[status]; !ok || count != 0 {
			t.Errorf("status %s = %d (present: %v), want 0", status, count, ok)
		}
	}
	if report.AvgCloseSeconds != 0 || report.CommentsTotal != 0 {
		t.Errorf("avg = %v, comments = %d, want zeros", report.AvgCloseSeconds, report.CommentsTotal)
	}
}

func TestReportAccess(t *testing.T) {
	f := buildFixture(t)
	ctx := context.Background()

	for name, actor := range map[string]uint64{"owner": f.owner, "admin": f.admin} {
		if _, err := f.service.Get(ctx, actor, f.teamA); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	cases := map[string]struct {
		actor uint64
		team  uint64
		kind  apperr.Kind
	}{
		"plain member":                  {f.member, f.teamA, apperr.Forbidden},
		"outsider":                      {f.outside, f.teamA, apperr.NotFound},
		"owner of A reads team B":       {f.owner, f.teamB, apperr.NotFound},
		"admin of A reads team C":       {f.admin, f.teamC, apperr.NotFound},
		"unknown team":                  {f.owner, 999999, apperr.NotFound},
		"member of B and A (assignees)": {f.users[1], f.teamA, apperr.Forbidden},
	}
	for name, tc := range cases {
		_, err := f.service.Get(ctx, tc.actor, tc.team)
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != tc.kind {
			t.Errorf("%s: err = %v, want kind %v", name, err, tc.kind)
		}
	}
}
