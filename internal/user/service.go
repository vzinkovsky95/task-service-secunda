// Единая ошибка входа для «нет пользователя» и «неверный пароль»; при
// отсутствии пользователя пароль сверяется с фиктивным хешем, чтобы время
// ответа не выдавало наличие email.

package user

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/db"
)

type Service struct {
	repo      *Repository
	tokens    *auth.Tokens
	dummyHash []byte
}

func NewService(repo *Repository, tokens *auth.Tokens) (*Service, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, tokens: tokens, dummyHash: dummy}, nil
}

func (s *Service) Register(ctx context.Context, rawEmail, password, rawName string) (User, error) {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return User{}, err
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	name, err := normalizeName(rawName)
	if err != nil {
		return User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	id, err := s.repo.Create(ctx, email, string(hash), name)
	if db.IsDuplicate(err) {
		return User{}, apperr.New(apperr.Conflict, "email_taken", "email is already registered")
	}
	if err != nil {
		return User{}, err
	}
	return s.repo.ByID(ctx, id)
}

func (s *Service) Login(ctx context.Context, rawEmail, password string) (string, time.Time, error) {
	invalidCredentials := apperr.New(apperr.Unauthorized, "invalid_credentials", "email or password is incorrect")

	email, err := normalizeEmail(rawEmail)
	if err != nil {
		bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return "", time.Time{}, invalidCredentials
	}

	u, err := s.repo.ByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return "", time.Time{}, invalidCredentials
	}
	if err != nil {
		return "", time.Time{}, err
	}

	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", time.Time{}, invalidCredentials
	}
	return s.tokens.Issue(u.ID)
}
