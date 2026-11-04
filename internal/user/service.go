// Единая ошибка входа для «нет пользователя» и «неверный пароль»; при
// отсутствии пользователя пароль сверяется с фиктивным хешем, чтобы время
// ответа не выдавало наличие email. Хеш задан готовой константой, а не считается
// при старте; его стоимость обязана совпадать с bcrypt.DefaultCost, иначе
// сравнение времени теряет смысл, и это проверяет тест.

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

const dummyHash = "$2a$10$2ldZdiocjjTABWZ5bVoWw.nhApsyiUU/JCdOpeQSEqOJxhxVQE6Wq"

type Service struct {
	repo   *Repository
	tokens *auth.Tokens
}

func NewService(repo *Repository, tokens *auth.Tokens) *Service {
	return &Service{repo: repo, tokens: tokens}
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
		bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return "", time.Time{}, invalidCredentials
	}

	u, err := s.repo.ByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
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
