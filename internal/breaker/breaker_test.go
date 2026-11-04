package breaker

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newBreaker() (*Breaker, *clock) {
	c := &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return New(Config{
		Threshold: 3,
		Cooldown:  10 * time.Second,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:       c.Now,
	}), c
}

func call(t *testing.T, b *Breaker, outcome Outcome) {
	t.Helper()
	done, err := b.Allow()
	if err != nil {
		t.Fatalf("call rejected: %v", err)
	}
	done(outcome)
}

func tripOpen(t *testing.T, b *Breaker) {
	t.Helper()
	for range 3 {
		call(t, b, Failure)
	}
	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
}

func TestOpensAfterThresholdOfConsecutiveFailures(t *testing.T) {
	b, _ := newBreaker()

	call(t, b, Failure)
	call(t, b, Failure)
	if b.State() != Closed {
		t.Fatalf("opened too early: %v", b.State())
	}
	call(t, b, Failure)
	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	b, _ := newBreaker()

	call(t, b, Failure)
	call(t, b, Failure)
	call(t, b, Success)
	call(t, b, Failure)
	call(t, b, Failure)
	if b.State() != Closed {
		t.Errorf("non-consecutive failures opened the breaker: %v", b.State())
	}
}

func TestIgnoredOutcomeDoesNotCount(t *testing.T) {
	b, _ := newBreaker()
	for range 10 {
		call(t, b, Ignore)
	}
	if b.State() != Closed {
		t.Errorf("state = %v", b.State())
	}
}

func TestOpenRejectsUntilCooldownPasses(t *testing.T) {
	b, clock := newBreaker()
	tripOpen(t, b)

	if _, err := b.Allow(); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v, want ErrOpen", err)
	}
	clock.Advance(9 * time.Second)
	if _, err := b.Allow(); !errors.Is(err, ErrOpen) {
		t.Fatalf("rejected call after 9s: err = %v", err)
	}
	clock.Advance(time.Second)
	if _, err := b.Allow(); err != nil {
		t.Fatalf("probe rejected after cooldown: %v", err)
	}
}

func TestHalfOpenAllowsExactlyOneProbe(t *testing.T) {
	b, clock := newBreaker()
	tripOpen(t, b)
	clock.Advance(10 * time.Second)

	var allowed, rejected int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.Allow()
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				allowed++
			} else {
				rejected++
			}
		}()
	}
	wg.Wait()

	if allowed != 1 || rejected != 19 {
		t.Errorf("allowed = %d, rejected = %d, want 1 and 19", allowed, rejected)
	}
}

func TestSuccessfulProbeClosesTheBreaker(t *testing.T) {
	b, clock := newBreaker()
	tripOpen(t, b)
	clock.Advance(10 * time.Second)

	call(t, b, Success)

	if b.State() != Closed {
		t.Fatalf("state = %v, want closed", b.State())
	}
	call(t, b, Failure)
	call(t, b, Failure)
	if b.State() != Closed {
		t.Error("failure count was not reset when the breaker closed")
	}
}

func TestFailedProbeReopensWithFreshCooldown(t *testing.T) {
	b, clock := newBreaker()
	tripOpen(t, b)
	clock.Advance(10 * time.Second)

	call(t, b, Failure)

	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
	clock.Advance(9 * time.Second)
	if _, err := b.Allow(); !errors.Is(err, ErrOpen) {
		t.Error("cooldown was not restarted after the failed probe")
	}
}

func TestIgnoredProbeFreesTheSlotWithoutChangingState(t *testing.T) {
	b, clock := newBreaker()
	tripOpen(t, b)
	clock.Advance(10 * time.Second)

	call(t, b, Ignore)

	if b.State() != HalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	if _, err := b.Allow(); err != nil {
		t.Errorf("next probe rejected: %v", err)
	}
}

func TestStaleResultFromEarlierStateIsIgnored(t *testing.T) {
	b, clock := newBreaker()

	slow, err := b.Allow()
	if err != nil {
		t.Fatal(err)
	}
	tripOpen(t, b)
	clock.Advance(10 * time.Second)
	call(t, b, Success)

	slow(Failure)

	if b.State() != Closed {
		t.Errorf("a result from before the state change moved the breaker to %v", b.State())
	}
}
