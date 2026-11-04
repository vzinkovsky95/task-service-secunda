package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
)

type ctxKey struct{}

func Require(tokens *Tokens) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				unauthorized(w, r)
				return
			}
			userID, err := tokens.Parse(raw)
			if err != nil {
				unauthorized(w, r)
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, userID)))
		}
	}
}

func UserID(ctx context.Context) uint64 {
	id, _ := ctx.Value(ctxKey{}).(uint64)
	return id
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	httpx.WriteServiceError(w, r, apperr.New(apperr.Unauthorized, "unauthorized", "valid bearer token is required"))
}
