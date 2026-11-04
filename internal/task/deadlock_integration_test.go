//go:build integration

package task_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
	"github.com/vzinkovsky95/task-service-secunda/internal/testdb"
)

const (
	fillerTasks   = 20000
	removedTasks  = 1500
	deadlockRound = 30
)

func TestReassignRacingMemberRemovalDoesNotDeadlock(t *testing.T) {
	pool := testdb.New(t)
	seed := testdb.NewSeeder(t, pool)

	lists := cache.NewTaskLists(cache.NewClient(miniredis.RunT(t).Addr()))
	teamRepo, taskRepo := team.NewRepository(), task.NewRepository()
	teams := team.NewService(pool, teamRepo, taskRepo, lists)
	tasks := task.NewService(pool, taskRepo, teamRepo, lists)

	owner, admin, carol := seed.User("owner"), seed.User("admin"), seed.User("carol")
	teamID := seed.Team("T", owner)
	seed.Member(teamID, admin, "admin")
	seed.Member(teamID, carol, "member")

	for _, assignee := range []uint64{owner, admin, carol} {
		seed.BulkTasks(teamID, owner, &assignee, fillerTasks/3)
	}

	var logs testdb.Logs
	ctx := logs.Context(context.Background())

	for round := 1; round <= deadlockRound; round++ {
		bob := seed.User("bob")
		seed.Member(teamID, bob, "member")
		seed.BulkTasks(teamID, owner, &bob, removedTasks)

		var taskID uint64
		if err := pool.QueryRow("SELECT MIN(id) FROM tasks WHERE team_id = ? AND assignee_id = ?", teamID, bob).Scan(&taskID); err != nil {
			t.Fatal(err)
		}

		var updateErr, removeErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, updateErr = tasks.Update(ctx, admin, taskID, task.UpdateInput{
				Title: "bulk", Status: task.StatusTodo, AssigneeID: &carol, Version: 1,
			})
		}()
		go func() {
			defer wg.Done()
			<-start
			removeErr = teams.RemoveMember(ctx, owner, teamID, bob)
		}()
		close(start)
		wg.Wait()

		if removeErr != nil {
			t.Fatalf("round %d: remove member: %v", round, removeErr)
		}
		if updateErr != nil {
			var appErr *apperr.Error
			if !errors.As(updateErr, &appErr) || appErr.Code != "version_conflict" {
				t.Fatalf("round %d: update: %v", round, updateErr)
			}
		}
	}

	if retries := logs.DeadlockRetries(); retries != 0 {
		t.Errorf("%d transactions hit a deadlock and were retried; lock order is broken", retries)
	}
	assertNoTasksAssignedToNonMembers(t, pool, teamID)
}

func assertNoTasksAssignedToNonMembers(t *testing.T, pool *sql.DB, teamID uint64) {
	t.Helper()
	var orphaned int
	err := pool.QueryRow(`SELECT COUNT(*) FROM tasks t
		LEFT JOIN team_members m ON m.team_id = t.team_id AND m.user_id = t.assignee_id
		WHERE t.team_id = ? AND t.assignee_id IS NOT NULL AND m.user_id IS NULL`, teamID).Scan(&orphaned)
	if err != nil {
		t.Fatal(err)
	}
	if orphaned != 0 {
		t.Errorf("%d tasks are still assigned to users who are no longer members", orphaned)
	}
}
