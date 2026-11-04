package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueAndParse(t *testing.T) {
	tokens := NewTokens("secret", time.Hour)

	raw, _, err := tokens.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := tokens.Parse(raw)
	if err != nil || userID != 42 {
		t.Fatalf("userID = %d, err = %v", userID, err)
	}
}

func TestParseRejects(t *testing.T) {
	tokens := NewTokens("secret", time.Hour)
	expired, _, _ := NewTokens("secret", -time.Minute).Issue(1)
	foreign, _, _ := NewTokens("other", time.Hour).Issue(1)
	noneAlg, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject:   "1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	noExpiry, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "1"}).SignedString([]byte("secret"))

	cases := map[string]string{
		"expired":        expired,
		"foreign secret": foreign,
		"alg none":       noneAlg,
		"no expiry":      noExpiry,
		"garbage":        "not-a-token",
		"empty":          "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tokens.Parse(raw); err == nil {
				t.Fatal("token accepted")
			}
		})
	}
}

func TestRequire(t *testing.T) {
	tokens := NewTokens("secret", time.Hour)
	valid, _, _ := tokens.Issue(7)

	var seen uint64
	handler := Require(tokens)(func(w http.ResponseWriter, r *http.Request) {
		seen = UserID(r.Context())
	})

	cases := map[string]struct {
		header string
		status int
	}{
		"no header":    {"", http.StatusUnauthorized},
		"wrong scheme": {"Basic " + valid, http.StatusUnauthorized},
		"bad token":    {"Bearer nope", http.StatusUnauthorized},
		"valid":        {"Bearer " + valid, http.StatusOK},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			seen = 0
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d", rec.Code)
			}
			if tc.status == http.StatusOK && seen != 7 {
				t.Errorf("user id in context = %d", seen)
			}
			if tc.status != http.StatusOK && seen != 0 {
				t.Error("handler ran without valid token")
			}
		})
	}
}
