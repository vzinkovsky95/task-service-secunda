//go:build integration

package team_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
	"github.com/vzinkovsky95/task-service-secunda/internal/testdb"
)

const membershipRounds = 40

type membershipFixture struct {
	pool   *sql.DB
	seed   *testdb.Seeder
	teams  *team.Service
	logs   *testdb.Logs
	ctx    context.Context
	owner  uint64
	teamID uint64
}

func newMembershipFixture(t *testing.T) *membershipFixture {
	t.Helper()
	pool := testdb.New(t)
	seed := testdb.NewSeeder(t, pool)

	lists := testdb.NewLists(t)
	logs := &testdb.Logs{}
	f := &membershipFixture{
		pool:  pool,
		seed:  seed,
		teams: team.NewService(pool, team.NewRepository(), task.NewRepository(), lists),
		logs:  logs,
		ctx:   logs.Context(context.Background()),
		owner: seed.User("owner"),
	}
	f.teamID = seed.Team("T", f.owner)
	return f
}

func race(first, second func()) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, run := range []func(){first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run()
		}()
	}
	close(start)
	wg.Wait()
}

func (f *membershipFixture) countMembers(t *testing.T, userIDs []uint64) int {
	t.Helper()
	var total int
	for _, id := range userIDs {
		var present int
		if err := f.pool.QueryRow("SELECT COUNT(*) FROM team_members WHERE team_id = ? AND user_id = ?", f.teamID, id).Scan(&present); err != nil {
			t.Fatal(err)
		}
		total += present
	}
	return total
}

func TestInviteRacingMemberRemovalDoesNotDeadlock(t *testing.T) {
	f := newMembershipFixture(t)

	var invitees, leavers []uint64
	for round := 1; round <= membershipRounds; round++ {
		invitee, leaver := f.seed.User("invitee"), f.seed.User("leaver")
		f.seed.Member(f.teamID, leaver, "member")
		invitees, leavers = append(invitees, invitee), append(leavers, leaver)

		var inviteErr, removeErr error
		race(
			func() { _, inviteErr = f.teams.Invite(f.ctx, f.owner, f.teamID, f.seed.EmailOf(invitee), team.Member) },
			func() { removeErr = f.teams.RemoveMember(f.ctx, f.owner, f.teamID, leaver) },
		)
		if inviteErr != nil || removeErr != nil {
			t.Fatalf("round %d: invite: %v, remove: %v", round, inviteErr, removeErr)
		}
	}

	if retries := f.logs.DeadlockRetries(); retries != 0 {
		t.Errorf("%d transactions hit a deadlock and were retried; lock order is broken", retries)
	}
	if got := f.countMembers(t, invitees); got != len(invitees) {
		t.Errorf("%d of %d invitees are members", got, len(invitees))
	}
	if got := f.countMembers(t, leavers); got != 0 {
		t.Errorf("%d removed members are still in the team", got)
	}
}

func TestRoleChangeRacingMemberRemovalDoesNotDeadlock(t *testing.T) {
	f := newMembershipFixture(t)

	for round := 1; round <= membershipRounds; round++ {
		promoted, leaver := f.seed.User("promoted"), f.seed.User("leaver")
		f.seed.Member(f.teamID, promoted, "member")
		f.seed.Member(f.teamID, leaver, "member")

		var changeErr, removeErr error
		race(
			func() { _, changeErr = f.teams.ChangeRole(f.ctx, f.owner, f.teamID, promoted, team.Admin) },
			func() { removeErr = f.teams.RemoveMember(f.ctx, f.owner, f.teamID, leaver) },
		)
		if changeErr != nil || removeErr != nil {
			t.Fatalf("round %d: change role: %v, remove: %v", round, changeErr, removeErr)
		}
	}

	if retries := f.logs.DeadlockRetries(); retries != 0 {
		t.Errorf("%d transactions hit a deadlock and were retried; lock order is broken", retries)
	}
}

func TestRoleChangeAndRemovalOfTheSameMemberStayConsistent(t *testing.T) {
	f := newMembershipFixture(t)

	for round := 1; round <= membershipRounds; round++ {
		target := f.seed.User("target")
		f.seed.Member(f.teamID, target, "member")

		var changeErr, removeErr error
		race(
			func() { _, changeErr = f.teams.ChangeRole(f.ctx, f.owner, f.teamID, target, team.Admin) },
			func() { removeErr = f.teams.RemoveMember(f.ctx, f.owner, f.teamID, target) },
		)

		var appErr *apperr.Error
		if removeErr != nil {
			t.Fatalf("round %d: remove: %v", round, removeErr)
		}
		if changeErr != nil && (!errors.As(changeErr, &appErr) || appErr.Code != "member_not_found") {
			t.Fatalf("round %d: change role: %v", round, changeErr)
		}
	}

	if retries := f.logs.DeadlockRetries(); retries != 0 {
		t.Errorf("%d transactions hit a deadlock and were retried", retries)
	}
}
