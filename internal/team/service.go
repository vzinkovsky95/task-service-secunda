// Порядок проверок в операциях над участниками: сначала видимость команды
// (не участник -> 404), затем право действующего (-> 403), и только потом
// поиск целевого пользователя, чтобы без права нельзя было выяснять, какие
// email зарегистрированы. Роли читаются под блокировкой внутри транзакции.
//
// Порядок блокировок общий с пакетом task: строка команды, затем строки
// участников, затем строки задач. Удаление участника берёт команду FOR UPDATE и
// не пересекается с созданием и обновлением задач. Приглашение берёт её FOR
// SHARE: вставка в team_members и так берёт эту строку по внешнему ключу, и
// брать её позже значит создать цикл с удалением. Смене роли команда не нужна:
// UPDATE роли внешние ключи не проверяет, и цикла нет.

package team

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

const maxNameRunes = 255

type TaskReleaser interface {
	ReleaseAssignee(ctx context.Context, tx *sql.Tx, teamID, userID, actorID uint64) error
}

type ListInvalidator interface {
	InvalidateTeam(ctx context.Context, teamID uint64)
}

type Service struct {
	pool  *sql.DB
	repo  *Repository
	tasks TaskReleaser
	lists ListInvalidator
}

func NewService(pool *sql.DB, repo *Repository, tasks TaskReleaser, lists ListInvalidator) *Service {
	return &Service{pool: pool, repo: repo, tasks: tasks, lists: lists}
}

type MemberRole struct {
	TeamID uint64
	UserID uint64
	Role   Role
}

func (s *Service) Create(ctx context.Context, userID uint64, rawName string) (Membership, error) {
	name := strings.TrimSpace(rawName)
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return Membership{}, apperr.New(apperr.Invalid, "validation_error", "name must be 1 to 255 characters long")
	}

	var created Team
	err := db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		id, err := s.repo.InsertTeam(ctx, tx, name, userID)
		if err != nil {
			return err
		}
		if err := s.repo.AddMember(ctx, tx, id, userID, Owner); err != nil {
			return err
		}
		created, err = s.repo.Team(ctx, tx, id)
		return err
	})
	if err != nil {
		return Membership{}, err
	}
	return Membership{Team: created, Role: Owner}, nil
}

func (s *Service) List(ctx context.Context, userID uint64) ([]Membership, error) {
	return s.repo.ListForUser(ctx, s.pool, userID)
}

func (s *Service) Invite(ctx context.Context, actorID, teamID uint64, email string, role Role) (MemberRole, error) {
	if role == "" {
		role = Member
	}
	if !isAssignable(role) {
		return MemberRole{}, apperr.New(apperr.Invalid, "validation_error", "role must be admin or member")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return MemberRole{}, apperr.New(apperr.Invalid, "validation_error", "email is required")
	}

	var invited MemberRole
	err := db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		if err := s.repo.ShareTeam(ctx, tx, teamID); err != nil {
			return err
		}
		roles, err := s.repo.LockRoles(ctx, tx, teamID, actorID)
		if err != nil {
			return err
		}
		actorRole, ok := roles[actorID]
		if !ok {
			return ErrTeamNotFound
		}
		if !canInvite(actorRole, role) {
			return apperr.New(apperr.Forbidden, "forbidden", "your role cannot invite with this role")
		}

		userID, err := s.repo.UserIDByEmail(ctx, tx, email)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.New(apperr.NotFound, "user_not_found", "no user with this email")
		}
		if err != nil {
			return err
		}

		err = s.repo.AddMember(ctx, tx, teamID, userID, role)
		if db.IsDuplicate(err) {
			return apperr.New(apperr.Conflict, "already_member", "user is already a member of this team")
		}
		if err != nil {
			return err
		}
		invited = MemberRole{TeamID: teamID, UserID: userID, Role: role}
		return nil
	})
	return invited, err
}

func (s *Service) ChangeRole(ctx context.Context, actorID, teamID, targetID uint64, role Role) (MemberRole, error) {
	if !isAssignable(role) {
		return MemberRole{}, apperr.New(apperr.Invalid, "validation_error", "role must be admin or member")
	}

	err := db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		roles, err := s.repo.LockRoles(ctx, tx, teamID, actorID, targetID)
		if err != nil {
			return err
		}
		actorRole, ok := roles[actorID]
		if !ok {
			return ErrTeamNotFound
		}
		if !canChangeRoles(actorRole) {
			return apperr.New(apperr.Forbidden, "forbidden", "only the owner can change roles")
		}
		targetRole, ok := roles[targetID]
		if !ok {
			return errMemberNotFound
		}
		if !canChangeRoleOf(targetRole) {
			return apperr.New(apperr.Forbidden, "forbidden", "the owner role cannot be changed")
		}
		return s.repo.SetRole(ctx, tx, teamID, targetID, role)
	})
	if err != nil {
		return MemberRole{}, err
	}
	return MemberRole{TeamID: teamID, UserID: targetID, Role: role}, nil
}

func (s *Service) RemoveMember(ctx context.Context, actorID, teamID, targetID uint64) error {
	err := db.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		if err := s.repo.LockTeam(ctx, tx, teamID); err != nil {
			return err
		}
		roles, err := s.repo.LockRoles(ctx, tx, teamID, actorID, targetID)
		if err != nil {
			return err
		}
		actorRole, ok := roles[actorID]
		if !ok {
			return ErrTeamNotFound
		}
		if !canRemoveAnyone(actorRole) {
			return apperr.New(apperr.Forbidden, "forbidden", "your role cannot remove members")
		}
		if actorID == targetID {
			return apperr.New(apperr.Forbidden, "forbidden", "you cannot remove yourself")
		}
		targetRole, ok := roles[targetID]
		if !ok {
			return errMemberNotFound
		}
		if !canRemove(actorRole, targetRole) {
			return apperr.New(apperr.Forbidden, "forbidden", "your role cannot remove this member")
		}
		if err := s.tasks.ReleaseAssignee(ctx, tx, teamID, targetID, actorID); err != nil {
			return err
		}
		return s.repo.RemoveMember(ctx, tx, teamID, targetID)
	})
	if err != nil {
		return err
	}
	s.lists.InvalidateTeam(ctx, teamID)
	return nil
}

var ErrTeamNotFound = apperr.New(apperr.NotFound, "team_not_found", "team not found")

var errMemberNotFound = apperr.New(apperr.NotFound, "member_not_found", "member not found")
