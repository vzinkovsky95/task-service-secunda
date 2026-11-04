package ratelimit

import (
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
)

type Subject func(r *http.Request) string

func ByClientIP(trusted []netip.Prefix) Subject {
	return func(r *http.Request) string {
		return ClientIP(r, trusted)
	}
}

func (l *Limiter) Middleware(rule Rule, subject Subject) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			decision := l.Allow(r.Context(), rule, subject(r))
			if decision.Allowed {
				next(w, r)
				return
			}

			retryAfter := int(decision.RetryAfter.Round(time.Second).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			httpx.WriteServiceError(w, r, apperr.New(apperr.TooManyRequests, "rate_limited", "too many requests, retry later").
				WithDetails(map[string]any{"retry_after_seconds": retryAfter}))
		}
	}
}
