//go:build integration

// Сидер вставляет данные напрямую в таблицы, минуя сервисы: так тест задаёт
// created_at и closed_at как угодно, в том числе старше 30 дней.

package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

type Seeder struct {
	t    *testing.T
	pool *sql.DB
	seq  int
}

func NewSeeder(t *testing.T, pool *sql.DB) *Seeder {
	return &Seeder{t: t, pool: pool}
}

func (s *Seeder) exec(query string, args ...any) uint64 {
	s.t.Helper()
	result, err := s.pool.ExecContext(context.Background(), query, args...)
	if err != nil {
		s.t.Fatalf("seed: %v\n%s", err, query)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}

func (s *Seeder) User(name string) uint64 {
	s.seq++
	email := fmt.Sprintf("user%d@seed.test", s.seq)
	return s.exec("INSERT INTO users (email, password_hash, name) VALUES (?, 'x', ?)", email, name)
}

func (s *Seeder) Team(name string, ownerID uint64) uint64 {
	id := s.exec("INSERT INTO teams (name, created_by) VALUES (?, ?)", name, ownerID)
	s.Member(id, ownerID, "owner")
	return id
}

func (s *Seeder) Member(teamID, userID uint64, role string) {
	s.exec("INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)", teamID, userID, role)
}

type Task struct {
	TeamID     uint64
	CreatedBy  uint64
	AssigneeID *uint64
	Status     string
	CreatedAt  time.Time
	ClosedAt   *time.Time
}

func (s *Seeder) Task(task Task) uint64 {
	return s.exec(`INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at)
		VALUES (?, 'seeded', '', ?, ?, ?, ?, ?, ?)`,
		task.TeamID, task.Status, task.CreatedBy, task.AssigneeID, task.CreatedAt, task.CreatedAt, task.ClosedAt)
}

func (s *Seeder) Comments(taskID, userID uint64, count int) {
	for i := 0; i < count; i++ {
		s.exec("INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, 'c')", taskID, userID)
	}
}

func (s *Seeder) BulkTasks(teamID, createdBy uint64, assigneeID *uint64, count int) {
	s.t.Helper()
	ctx := context.Background()

	conn, err := s.pool.Conn(ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET SESSION cte_max_recursion_depth = %d", count+10)); err != nil {
		s.t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO tasks (team_id, title, description, created_by, assignee_id)
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		SELECT ?, 'bulk', '', ?, ? FROM n`, count, teamID, createdBy, assigneeID)
	if err != nil {
		s.t.Fatal(err)
	}
}

func (s *Seeder) EmailOf(userID uint64) string {
	s.t.Helper()
	var email string
	if err := s.pool.QueryRow("SELECT email FROM users WHERE id = ?", userID).Scan(&email); err != nil {
		s.t.Fatal(err)
	}
	return email
}
