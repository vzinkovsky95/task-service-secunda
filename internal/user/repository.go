package user

import (
	"context"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

type User struct {
	ID           uint64
	Email        string
	PasswordHash string
	Name         string
	CreatedAt    time.Time
}

type Repository struct {
	q db.Querier
}

func NewRepository(q db.Querier) *Repository {
	return &Repository{q: q}
}

func (r *Repository) Create(ctx context.Context, email, passwordHash, name string) (uint64, error) {
	result, err := r.q.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?)", email, passwordHash, name)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func (r *Repository) ByID(ctx context.Context, id uint64) (User, error) {
	return r.scan(r.q.QueryRowContext(ctx,
		"SELECT id, email, password_hash, name, created_at FROM users WHERE id = ?", id))
}

func (r *Repository) ByEmail(ctx context.Context, email string) (User, error) {
	return r.scan(r.q.QueryRowContext(ctx,
		"SELECT id, email, password_hash, name, created_at FROM users WHERE email = ?", email))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Repository) scan(row rowScanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt)
	return u, err
}
