package team

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

type Team struct {
	ID        uint64
	Name      string
	CreatedBy uint64
	CreatedAt time.Time
}

type Membership struct {
	Team Team
	Role Role
}

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) InsertTeam(ctx context.Context, q db.Querier, name string, createdBy uint64) (uint64, error) {
	result, err := q.ExecContext(ctx, "INSERT INTO teams (name, created_by) VALUES (?, ?)", name, createdBy)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func (r *Repository) Team(ctx context.Context, q db.Querier, id uint64) (Team, error) {
	var t Team
	err := q.QueryRowContext(ctx, "SELECT id, name, created_by, created_at FROM teams WHERE id = ?", id).
		Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt)
	return t, err
}

func (r *Repository) ShareTeam(ctx context.Context, q db.Querier, teamID uint64) error {
	return r.lockTeam(ctx, q, teamID, "FOR SHARE")
}

func (r *Repository) LockTeam(ctx context.Context, q db.Querier, teamID uint64) error {
	return r.lockTeam(ctx, q, teamID, "FOR UPDATE")
}

func (r *Repository) lockTeam(ctx context.Context, q db.Querier, teamID uint64, lock string) error {
	var id uint64
	err := q.QueryRowContext(ctx, "SELECT id FROM teams WHERE id = ? "+lock, teamID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTeamNotFound
	}
	return err
}

func (r *Repository) AddMember(ctx context.Context, q db.Querier, teamID, userID uint64, role Role) error {
	_, err := q.ExecContext(ctx,
		"INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)", teamID, userID, string(role))
	return err
}

func (r *Repository) SetRole(ctx context.Context, q db.Querier, teamID, userID uint64, role Role) error {
	_, err := q.ExecContext(ctx,
		"UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?", string(role), teamID, userID)
	return err
}

func (r *Repository) RemoveMember(ctx context.Context, q db.Querier, teamID, userID uint64) error {
	_, err := q.ExecContext(ctx, "DELETE FROM team_members WHERE team_id = ? AND user_id = ?", teamID, userID)
	return err
}

func (r *Repository) LockRoles(ctx context.Context, q db.Querier, teamID uint64, userIDs ...uint64) (map[uint64]Role, error) {
	return r.roles(ctx, q, teamID, "FOR UPDATE", userIDs)
}

func (r *Repository) ShareRoles(ctx context.Context, q db.Querier, teamID uint64, userIDs ...uint64) (map[uint64]Role, error) {
	return r.roles(ctx, q, teamID, "FOR SHARE", userIDs)
}

func (r *Repository) RoleOf(ctx context.Context, q db.Querier, teamID, userID uint64) (Role, bool, error) {
	var role string
	err := q.QueryRowContext(ctx, "SELECT role FROM team_members WHERE team_id = ? AND user_id = ?", teamID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return Role(role), err == nil, err
}

func (r *Repository) roles(ctx context.Context, q db.Querier, teamID uint64, lock string, userIDs []uint64) (map[uint64]Role, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(userIDs)), ",")
	args := []any{teamID}
	for _, id := range userIDs {
		args = append(args, id)
	}

	rows, err := q.QueryContext(ctx,
		"SELECT user_id, role FROM team_members WHERE team_id = ? AND user_id IN ("+placeholders+") ORDER BY user_id "+lock,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := map[uint64]Role{}
	for rows.Next() {
		var userID uint64
		var role string
		if err := rows.Scan(&userID, &role); err != nil {
			return nil, err
		}
		roles[userID] = Role(role)
	}
	return roles, rows.Err()
}

func (r *Repository) UserIDByEmail(ctx context.Context, q db.Querier, email string) (uint64, error) {
	var id uint64
	err := q.QueryRowContext(ctx, "SELECT id FROM users WHERE email = ?", email).Scan(&id)
	return id, err
}

func (r *Repository) ListForUser(ctx context.Context, q db.Querier, userID uint64) ([]Membership, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT t.id, t.name, t.created_by, t.created_at, m.role
		FROM team_members m
		JOIN teams t ON t.id = m.team_id
		WHERE m.user_id = ?
		ORDER BY t.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memberships := []Membership{}
	for rows.Next() {
		var m Membership
		var role string
		if err := rows.Scan(&m.Team.ID, &m.Team.Name, &m.Team.CreatedBy, &m.Team.CreatedAt, &role); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}
