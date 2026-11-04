package user

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
)

const (
	minPasswordBytes = 8
	maxPasswordBytes = 72
	maxNameRunes     = 255
	maxEmailBytes    = 254
)

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > maxEmailBytes {
		return "", invalid("email is invalid")
	}
	return email, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordBytes || len(password) > maxPasswordBytes {
		return invalid("password must be 8 to 72 bytes long")
	}
	return nil
}

func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return "", invalid("name must be 1 to 255 characters long")
	}
	return name, nil
}

func invalid(message string) error {
	return apperr.New(apperr.Invalid, "validation_error", message)
}
