// Порядок блокировок везде один: строка команды, затем строки участников, затем
// строка задачи. Строка участника не разводит обновление задачи и удаление
// участника: прежний исполнитель при обновлении не блокируется, а снятие
// исполнителя идёт по индексу (team_id, assignee_id), тогда как обновление идёт
// по первичному ключу, то есть в обратном порядке. Поэтому создание и
// обновление берут команду FOR SHARE, а удаление участника FOR UPDATE: они не
// выполняются одновременно. На случай оставшихся конфликтов db.InTx повторяет
// транзакцию при deadlock. Права и версия проверяются по строке, прочитанной
// под блокировкой.

package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
	"github.com/vzinkovsky95/task-service-secunda/internal/db"
	"github.com/vzinkovsky95/task-service-secunda/internal/logx"
	"github.com/vzinkovsky95/task-service-secunda/internal/team"
)

var (
	errTaskNotFound      = apperr.New(apperr.NotFound, "task_not_found", "task not found")
	errAssigneeNotMember = apperr.New(apperr.Invalid, "assignee_not_member", "assignee must be a member of the task's team")
)

type Service struct {
	pool    *sql.DB
	repo    *Repository
	members *team.Repository
	lists   *cache.TaskLists
}

func NewService(pool *sql.DB, repo *Repository, members *team.Repository, lists *cache.TaskLists) *Service {
	return &Service{pool: pool, repo: repo, members: members, lists: lists}
}

func (s *Service) Create(ctx context.Context, actorID uint64, in CreateInput) (Task, error) {
	in, err := normalizeCreate(in)
	if err != nil {
		return Task{}, err
	}

	var created Task
	err = db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		if err := s.members.ShareTeam(ctx, tx, in.TeamID); err != nil {
			return err
		}
		ids := []uint64{actorID}
		if in.AssigneeID != nil {
			ids = append(ids, *in.AssigneeID)
		}
		roles, err := s.members.ShareRoles(ctx, tx, in.TeamID, ids...)
		if err != nil {
			return err
		}
		if _, ok := roles[actorID]; !ok {
			return team.ErrTeamNotFound
		}
		if in.AssigneeID != nil {
			if _, ok := roles[*in.AssigneeID]; !ok {
				return errAssigneeNotMember
			}
		}

		id, err := s.repo.Insert(ctx, tx, actorID, in)
		if err != nil {
			return err
		}
		if created, err = s.repo.Get(ctx, tx, id); err != nil {
			return err
		}
		return s.repo.AddHistory(ctx, tx, id, actorID, creationChanges(created))
	})
	if err != nil {
		return Task{}, err
	}
	s.lists.InvalidateTeam(ctx, in.TeamID)
	return created, nil
}

func (s *Service) Update(ctx context.Context, actorID, taskID uint64, in UpdateInput) (Task, error) {
	in, err := normalizeUpdate(in)
	if err != nil {
		return Task{}, err
	}

	teamID, err := s.repo.TeamID(ctx, s.pool, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, errTaskNotFound
	}
	if err != nil {
		return Task{}, err
	}

	var result Task
	var changed bool
	err = db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		changed = false
		if err := s.members.ShareTeam(ctx, tx, teamID); err != nil {
			return err
		}
		ids := []uint64{actorID}
		if in.AssigneeID != nil {
			ids = append(ids, *in.AssigneeID)
		}
		roles, err := s.members.ShareRoles(ctx, tx, teamID, ids...)
		if err != nil {
			return err
		}
		actorRole, ok := roles[actorID]
		if !ok {
			return errTaskNotFound
		}

		current, err := s.repo.Lock(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return apperr.New(apperr.Conflict, "version_conflict", "task was changed by someone else").
				WithDetails(map[string]any{"current_version": current.Version})
		}

		changes := diff(current, in)
		if len(changes) == 0 {
			result = current
			return nil
		}

		if denied := deniedFields(actorRole, relationsOf(current, actorID), changes.fields()); len(denied) > 0 {
			return apperr.New(apperr.Forbidden, "forbidden", "your role cannot change: "+joinFields(denied))
		}
		if _, changed := changes[FieldAssignee]; changed && in.AssigneeID != nil {
			if _, ok := roles[*in.AssigneeID]; !ok {
				return errAssigneeNotMember
			}
		}

		updated := apply(current, in)
		if change, changed := closedAtChange(current, updated); changed {
			changes[FieldClosedAt] = change
		}
		if err := s.repo.Save(ctx, tx, updated); err != nil {
			return err
		}
		if err := s.repo.AddHistory(ctx, tx, taskID, actorID, changes); err != nil {
			return err
		}
		if result, err = s.repo.Get(ctx, tx, taskID); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	if changed {
		s.lists.InvalidateTeam(ctx, teamID)
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, actorID uint64, f ListFilter) ([]Task, int, error) {
	_, ok, err := s.members.RoleOf(ctx, s.pool, f.TeamID, actorID)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, team.ErrTeamNotFound
	}

	data, slot := s.lists.Load(ctx, f.TeamID, f.cacheKey())
	if data != nil {
		var cached cachedList
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached.Tasks, cached.Total, nil
		}
		logx.From(ctx).Warn("task list cache entry is corrupt", "team_id", f.TeamID)
	}
	return s.listFromDB(ctx, slot, f)
}

func (s *Service) listFromDB(ctx context.Context, slot cache.Slot, f ListFilter) ([]Task, int, error) {
	tasks, total, err := s.repo.List(ctx, s.pool, f)
	if err != nil {
		return nil, 0, err
	}
	if payload, err := json.Marshal(cachedList{Tasks: tasks, Total: total}); err == nil {
		s.lists.Store(ctx, slot, payload)
	}
	return tasks, total, nil
}

func (s *Service) History(ctx context.Context, actorID, taskID uint64, limit, offset int) ([]HistoryEntry, int, error) {
	if err := s.requireVisible(ctx, s.pool, actorID, taskID, noLock); err != nil {
		return nil, 0, err
	}
	return s.repo.History(ctx, s.pool, taskID, limit, offset)
}

func (s *Service) Comments(ctx context.Context, actorID, taskID uint64, limit, offset int) ([]Comment, int, error) {
	if err := s.requireVisible(ctx, s.pool, actorID, taskID, noLock); err != nil {
		return nil, 0, err
	}
	return s.repo.Comments(ctx, s.pool, taskID, limit, offset)
}

func (s *Service) AddComment(ctx context.Context, actorID, taskID uint64, rawContent string) (Comment, error) {
	content, err := normalizeComment(rawContent)
	if err != nil {
		return Comment{}, err
	}

	var comment Comment
	err = db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		if err := s.requireVisible(ctx, tx, actorID, taskID, shareLock); err != nil {
			return err
		}
		comment, err = s.repo.AddComment(ctx, tx, taskID, actorID, content)
		return err
	})
	return comment, err
}

func (s *Service) requireVisible(ctx context.Context, q db.Querier, actorID, taskID uint64, lock string) error {
	visible, err := s.repo.IsTaskVisible(ctx, q, taskID, actorID, lock)
	if err != nil {
		return err
	}
	if !visible {
		return errTaskNotFound
	}
	return nil
}

func apply(current Task, in UpdateInput) Task {
	updated := current
	updated.Title = in.Title
	updated.Description = in.Description
	updated.Status = in.Status
	updated.AssigneeID = in.AssigneeID

	switch {
	case current.Status != StatusDone && in.Status == StatusDone:
		now := time.Now().UTC().Truncate(time.Microsecond)
		updated.ClosedAt = &now
	case current.Status == StatusDone && in.Status != StatusDone:
		updated.ClosedAt = nil
	}
	return updated
}

func closedAtChange(before, after Task) (Change, bool) {
	if (before.ClosedAt == nil) == (after.ClosedAt == nil) {
		return Change{}, false
	}
	return Change{Old: timeValue(before.ClosedAt), New: timeValue(after.ClosedAt)}, true
}

func creationChanges(t Task) Changes {
	return Changes{
		FieldTitle:       {Old: nil, New: t.Title},
		FieldDescription: {Old: nil, New: t.Description},
		FieldStatus:      {Old: nil, New: t.Status},
		FieldAssignee:    {Old: nil, New: idValue(t.AssigneeID)},
	}
}

func timeValue(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func joinFields(fields []Field) string {
	names := ""
	for i, field := range fields {
		if i > 0 {
			names += ", "
		}
		names += string(field)
	}
	return names
}
