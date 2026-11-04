package stats

import (
	"context"
	"database/sql"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
)

type Service struct {
	pool    *sql.DB
	members *team.Repository
	repo    *Repository
	now     func() time.Time
}

func NewService(pool *sql.DB, members *team.Repository, repo *Repository, now func() time.Time) *Service {
	return &Service{pool: pool, members: members, repo: repo, now: now}
}

func (s *Service) Get(ctx context.Context, actorID, teamID uint64) (Report, error) {
	role, ok, err := s.members.RoleOf(ctx, s.pool, teamID, actorID)
	if err != nil {
		return Report{}, err
	}
	if !ok {
		return Report{}, team.ErrTeamNotFound
	}
	if !team.CanViewStats(role) {
		return Report{}, apperr.New(apperr.Forbidden, "forbidden", "only the owner or an admin can view team statistics")
	}
	return s.repo.Report(ctx, s.pool, teamID, s.now())
}
