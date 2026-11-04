package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

const taskColumns = "id, team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version"

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var status string
	var assignee sql.NullInt64
	var closedAt sql.NullTime
	err := row.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &status, &t.CreatedBy, &assignee,
		&t.CreatedAt, &t.UpdatedAt, &closedAt, &t.Version)
	if err != nil {
		return Task{}, err
	}
	t.Status = Status(status)
	if assignee.Valid {
		id := uint64(assignee.Int64)
		t.AssigneeID = &id
	}
	if closedAt.Valid {
		t.ClosedAt = &closedAt.Time
	}
	return t, nil
}

func (r *Repository) Insert(ctx context.Context, q db.Querier, createdBy uint64, in CreateInput) (uint64, error) {
	result, err := q.ExecContext(ctx,
		"INSERT INTO tasks (team_id, title, description, created_by, assignee_id) VALUES (?, ?, ?, ?, ?)",
		in.TeamID, in.Title, in.Description, createdBy, in.AssigneeID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func (r *Repository) Get(ctx context.Context, q db.Querier, id uint64) (Task, error) {
	return scanTask(q.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id))
}

func (r *Repository) Lock(ctx context.Context, q db.Querier, id uint64) (Task, error) {
	return scanTask(q.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ? FOR UPDATE", id))
}

func (r *Repository) TeamID(ctx context.Context, q db.Querier, id uint64) (uint64, error) {
	var teamID uint64
	err := q.QueryRowContext(ctx, "SELECT team_id FROM tasks WHERE id = ?", id).Scan(&teamID)
	return teamID, err
}

func (r *Repository) Save(ctx context.Context, q db.Querier, t Task) error {
	_, err := q.ExecContext(ctx,
		"UPDATE tasks SET title = ?, description = ?, status = ?, assignee_id = ?, closed_at = ?, version = version + 1 WHERE id = ?",
		t.Title, t.Description, string(t.Status), t.AssigneeID, t.ClosedAt, t.ID)
	return err
}

func (r *Repository) List(ctx context.Context, q db.Querier, f ListFilter) ([]Task, int, error) {
	where := "team_id = ?"
	args := []any{f.TeamID}
	if f.Status != "" {
		where += " AND status = ?"
		args = append(args, string(f.Status))
	}
	if f.AssigneeID != nil {
		where += " AND assignee_id = ?"
		args = append(args, *f.AssigneeID)
	}

	var total int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.QueryContext(ctx,
		"SELECT "+taskColumns+" FROM tasks WHERE "+where+" ORDER BY id DESC LIMIT ? OFFSET ?",
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	return tasks, total, rows.Err()
}

func (r *Repository) AddHistory(ctx context.Context, q db.Querier, taskID, changedBy uint64, changes Changes) error {
	payload, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx,
		"INSERT INTO task_history (task_id, changed_by, changes) VALUES (?, ?, ?)", taskID, changedBy, string(payload))
	return err
}

func (r *Repository) History(ctx context.Context, q db.Querier, taskID uint64, limit, offset int) ([]HistoryEntry, int, error) {
	var total int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM task_history WHERE task_id = ?", taskID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.QueryContext(ctx,
		"SELECT id, task_id, changed_by, changes, created_at FROM task_history WHERE task_id = ? ORDER BY id LIMIT ? OFFSET ?",
		taskID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var changes []byte
		if err := rows.Scan(&e.ID, &e.TaskID, &e.ChangedBy, &changes, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		e.Changes = json.RawMessage(changes)
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

func (r *Repository) AddComment(ctx context.Context, q db.Querier, taskID, userID uint64, content string) (Comment, error) {
	result, err := q.ExecContext(ctx,
		"INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, ?)", taskID, userID, content)
	if err != nil {
		return Comment{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Comment{}, err
	}

	var c Comment
	err = q.QueryRowContext(ctx,
		"SELECT id, task_id, user_id, content, created_at FROM task_comments WHERE id = ?", id).
		Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt)
	return c, err
}

func (r *Repository) Comments(ctx context.Context, q db.Querier, taskID uint64, limit, offset int) ([]Comment, int, error) {
	var total int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM task_comments WHERE task_id = ?", taskID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.QueryContext(ctx,
		"SELECT id, task_id, user_id, content, created_at FROM task_comments WHERE task_id = ? ORDER BY id LIMIT ? OFFSET ?",
		taskID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	comments := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		comments = append(comments, c)
	}
	return comments, total, rows.Err()
}

func (r *Repository) IsTaskVisible(ctx context.Context, q db.Querier, taskID, userID uint64, lock string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `
		SELECT 1
		FROM tasks t
		JOIN team_members m ON m.team_id = t.team_id AND m.user_id = ?
		WHERE t.id = ? `+lock, userID, taskID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) ReleaseAssignee(ctx context.Context, tx *sql.Tx, teamID, userID, actorID uint64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO task_history (task_id, changed_by, changes)
		SELECT id, ?, JSON_OBJECT('assignee_id', JSON_OBJECT('old', assignee_id, 'new', NULL))
		FROM tasks
		WHERE team_id = ? AND assignee_id = ?`, actorID, teamID, userID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"UPDATE tasks SET assignee_id = NULL, version = version + 1 WHERE team_id = ? AND assignee_id = ?", teamID, userID)
	return err
}

const (
	noLock    = ""
	shareLock = "FOR SHARE OF m"
)
