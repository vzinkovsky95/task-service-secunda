package httpx

import (
	"net/http"
	"strconv"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type PageParams struct {
	Limit  int
	Offset int
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

func ParsePage(r *http.Request) (PageParams, error) {
	params := PageParams{Limit: defaultLimit}
	query := r.URL.Query()

	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxLimit {
			return PageParams{}, apperr.New(apperr.Invalid, "validation_error", "limit must be between 1 and 100")
		}
		params.Limit = limit
	}
	if raw := query.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return PageParams{}, apperr.New(apperr.Invalid, "validation_error", "offset must be zero or greater")
		}
		params.Offset = offset
	}
	return params, nil
}

func NewPage[T any](items []T, params PageParams, total int) Page[T] {
	return Page[T]{Items: items, Limit: params.Limit, Offset: params.Offset, Total: total}
}
