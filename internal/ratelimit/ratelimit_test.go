package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/vzinkovsky95/task-service-secunda/internal/breaker"
	"github.com/vzinkovsky95/task-service-secunda/internal/cache"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newLimiter(t *testing.T) (*Limiter, *fakeClock, *miniredis.Miniredis, *breaker.Breaker) {
	t.Helper()
	server := miniredis.RunT(t)
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 10, 0, time.UTC)}
	guard := breaker.New(breaker.Config{Threshold: 3, Cooldown: time.Minute, Now: clock.Now})
	return NewLimiter(cache.NewClient(server.Addr(), guard), clock.Now), clock, server, guard
}

var rule = Rule{Scope: "login", Limit: 3, Window: time.Minute}

func TestAllowsUpToLimitThenRejects(t *testing.T) {
	limiter, _, _, _ := newLimiter(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		if !limiter.Allow(ctx, rule, "1.1.1.1").Allowed {
			t.Fatalf("request %d rejected", i)
		}
	}
	decision := limiter.Allow(ctx, rule, "1.1.1.1")
	if decision.Allowed {
		t.Fatal("4th request allowed")
	}
	if decision.RetryAfter != 50*time.Second {
		t.Errorf("retry after = %v, want 50s (window ends at :00)", decision.RetryAfter)
	}
}

func TestSubjectsAndScopesAreIndependent(t *testing.T) {
	limiter, _, _, _ := newLimiter(t)
	ctx := context.Background()
	for range 3 {
		limiter.Allow(ctx, rule, "1.1.1.1")
	}

	if !limiter.Allow(ctx, rule, "2.2.2.2").Allowed {
		t.Error("another subject was limited")
	}
	other := Rule{Scope: "register", Limit: 3, Window: time.Minute}
	if !limiter.Allow(ctx, other, "1.1.1.1").Allowed {
		t.Error("another scope was limited")
	}
}

func TestNewWindowResetsTheCounter(t *testing.T) {
	limiter, clock, _, _ := newLimiter(t)
	ctx := context.Background()
	for range 4 {
		limiter.Allow(ctx, rule, "1.1.1.1")
	}

	clock.Advance(50 * time.Second)

	if !limiter.Allow(ctx, rule, "1.1.1.1").Allowed {
		t.Error("still limited in the next window")
	}
}

func TestCounterKeysAlwaysExpire(t *testing.T) {
	limiter, _, server, _ := newLimiter(t)

	limiter.Allow(context.Background(), rule, "1.1.1.1")

	keys := server.Keys()
	if len(keys) != 1 {
		t.Fatalf("keys = %v", keys)
	}
	if ttl := server.TTL(keys[0]); ttl != time.Minute {
		t.Errorf("ttl = %v, want 1m", ttl)
	}
}

func TestRedisDownAllowsRequestsAndOpenBreakerIsInstant(t *testing.T) {
	limiter, _, server, guard := newLimiter(t)
	ctx := context.Background()
	server.Close()

	for range 3 {
		if !limiter.Allow(ctx, rule, "1.1.1.1").Allowed {
			t.Fatal("request rejected while Redis is down")
		}
	}
	if guard.State() != breaker.Open {
		t.Fatalf("breaker state = %v, want open", guard.State())
	}

	started := time.Now()
	for range 50 {
		if !limiter.Allow(ctx, rule, "1.1.1.1").Allowed {
			t.Fatal("request rejected while the breaker is open")
		}
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Errorf("50 calls with an open breaker took %v", elapsed)
	}
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	limiter, _, _, _ := newLimiter(t)
	handler := limiter.Middleware(rule, func(*http.Request) string { return "s" })(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for i := 1; i <= 4; i++ {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodPost, "/login", nil))

		if i <= 3 && rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
		if i == 4 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Header().Get("Retry-After"); got != "50" {
				t.Errorf("Retry-After = %q", got)
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Error("429 is not a JSON error")
			}
		}
	}
}

func prefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	var result []netip.Prefix
	for _, cidr := range cidrs {
		result = append(result, netip.MustParsePrefix(cidr))
	}
	return result
}

func TestClientIP(t *testing.T) {
	proxies := prefixes(t, "10.0.0.0/8", "192.168.1.5/32")

	cases := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"direct client, no proxies configured", "203.0.113.7:5000", nil, nil, "203.0.113.7"},
		{"spoofed XFF is ignored without trusted proxies", "203.0.113.7:5000", []string{"1.2.3.4"}, nil, "203.0.113.7"},
		{"spoofed XFF is ignored from an untrusted peer", "203.0.113.7:5000", []string{"1.2.3.4"}, proxies, "203.0.113.7"},
		{"trusted proxy: client from XFF", "10.0.0.2:5000", []string{"203.0.113.7"}, proxies, "203.0.113.7"},
		{"client-supplied prefix is skipped, nearest untrusted wins", "10.0.0.2:5000", []string{"6.6.6.6, 203.0.113.7"}, proxies, "203.0.113.7"},
		{"chain of trusted proxies is skipped", "10.0.0.2:5000", []string{"203.0.113.7, 10.0.0.9, 192.168.1.5"}, proxies, "203.0.113.7"},
		{"several XFF header lines are joined", "10.0.0.2:5000", []string{"6.6.6.6", "203.0.113.7"}, proxies, "203.0.113.7"},
		{"all trusted: attribute to the peer", "10.0.0.2:5000", []string{"10.0.0.8"}, proxies, "10.0.0.2"},
		{"no XFF from a trusted proxy", "10.0.0.2:5000", nil, proxies, "10.0.0.2"},
		{"garbage in the chain is not guessed", "10.0.0.2:5000", []string{"not-an-ip"}, proxies, "10.0.0.2"},
		{"IPv4-mapped IPv6 peer", "[::ffff:203.0.113.7]:5000", nil, nil, "203.0.113.7"},
		{"IPv6 clients share a /64", "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:5000", nil, nil, "2001:db8:1:2::/64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			req.RemoteAddr = tc.remote
			for _, line := range tc.xff {
				req.Header.Add("X-Forwarded-For", line)
			}
			if got := ClientIP(req, tc.trusted); got != tc.want {
				t.Errorf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOtherIPv6SubnetsAreSeparate(t *testing.T) {
	a := httptest.NewRequest(http.MethodGet, "/", nil)
	a.RemoteAddr = "[2001:db8:1:2::1]:1"
	b := httptest.NewRequest(http.MethodGet, "/", nil)
	b.RemoteAddr = "[2001:db8:1:3::1]:1"

	if ClientIP(a, nil) == ClientIP(b, nil) {
		t.Error("different /64 subnets were merged")
	}
}
