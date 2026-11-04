package cache

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	miniserver "github.com/alicebob/miniredis/v2/server"
	"github.com/redis/go-redis/v9"
)

func newTestCache(t *testing.T) (*TaskLists, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	return NewTaskLists(NewClient(server.Addr())), server
}

func TestStoreThenLoadHits(t *testing.T) {
	lists, _ := newTestCache(t)
	ctx := context.Background()

	data, slot := lists.Load(ctx, 1, "f")
	if data != nil {
		t.Fatal("empty cache returned data")
	}
	lists.Store(ctx, slot, []byte("payload"))

	data, _ = lists.Load(ctx, 1, "f")
	if string(data) != "payload" {
		t.Errorf("data = %q", data)
	}
}

func TestFiltersAndTeamsDoNotMix(t *testing.T) {
	lists, _ := newTestCache(t)
	ctx := context.Background()

	_, slot := lists.Load(ctx, 1, "status=todo")
	lists.Store(ctx, slot, []byte("team1-todo"))

	if data, _ := lists.Load(ctx, 1, "status=done"); data != nil {
		t.Errorf("other filter got %q", data)
	}
	if data, _ := lists.Load(ctx, 2, "status=todo"); data != nil {
		t.Errorf("other team got %q", data)
	}
}

func TestInvalidateTeamHidesOldEntriesOfThatTeamOnly(t *testing.T) {
	lists, _ := newTestCache(t)
	ctx := context.Background()

	for _, team := range []uint64{1, 2} {
		_, slot := lists.Load(ctx, team, "f")
		lists.Store(ctx, slot, []byte("old"))
	}

	lists.InvalidateTeam(ctx, 1)

	if data, _ := lists.Load(ctx, 1, "f"); data != nil {
		t.Errorf("invalidated team still served %q", data)
	}
	if data, _ := lists.Load(ctx, 2, "f"); string(data) != "old" {
		t.Errorf("untouched team lost its entry: %q", data)
	}
}

func TestSlotTakenBeforeInvalidationNeverServedAfter(t *testing.T) {
	lists, _ := newTestCache(t)
	ctx := context.Background()

	_, staleSlot := lists.Load(ctx, 1, "f")
	lists.InvalidateTeam(ctx, 1)
	lists.Store(ctx, staleSlot, []byte("computed before the change"))

	if data, _ := lists.Load(ctx, 1, "f"); data != nil {
		t.Errorf("served %q computed before invalidation", data)
	}
}

func TestTTLOnListsButNotOnGenerationCounters(t *testing.T) {
	lists, server := newTestCache(t)
	ctx := context.Background()

	_, slot := lists.Load(ctx, 1, "f")
	lists.Store(ctx, slot, []byte("x"))
	lists.InvalidateTeam(ctx, 1)

	if ttl := server.TTL(slot.key); ttl != 5*time.Minute {
		t.Errorf("list ttl = %v", ttl)
	}
	if ttl := server.TTL(generationKey(1)); ttl != 0 {
		t.Errorf("generation counter has ttl %v", ttl)
	}
}

func TestRedisDownIsAMissNotAnError(t *testing.T) {
	lists, server := newTestCache(t)
	ctx := context.Background()
	server.Close()

	started := time.Now()
	data, slot := lists.Load(ctx, 1, "f")
	lists.Store(ctx, slot, []byte("x"))
	lists.InvalidateTeam(ctx, 1)

	if data != nil || slot.usable {
		t.Error("unavailable cache must report a miss with an unusable slot")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("calls took %v with Redis down", elapsed)
	}
}

func TestInvalidateIgnoresCancelledRequest(t *testing.T) {
	lists, server := newTestCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lists.InvalidateTeam(ctx, 1)

	if got, err := server.Get(generationKey(1)); err != nil || got != "1" {
		t.Errorf("generation = %q, err = %v", got, err)
	}
}

func TestFailedInvalidationIsRetriedBeforeNextRead(t *testing.T) {
	lists, server := newTestCache(t)
	ctx := context.Background()

	_, slot := lists.Load(ctx, 1, "f")
	lists.Store(ctx, slot, []byte("old"))

	server.Close()
	lists.InvalidateTeam(ctx, 1)
	if data, _ := lists.Load(ctx, 1, "f"); data != nil {
		t.Fatal("served data while Redis is down")
	}

	if err := server.Restart(); err != nil {
		t.Fatal(err)
	}
	data, newSlot := lists.Load(ctx, 1, "f")
	if data != nil {
		t.Errorf("served %q computed before the missed invalidation", data)
	}
	if newSlot.key == slot.key {
		t.Error("generation did not move after the missed invalidation")
	}

	lists.Store(ctx, newSlot, []byte("fresh"))
	if data, _ := lists.Load(ctx, 1, "f"); string(data) != "fresh" {
		t.Errorf("pending invalidation applied twice or entry lost: %q", data)
	}
}

func TestPendingInvalidationOfOneTeamDoesNotAffectAnother(t *testing.T) {
	lists, server := newTestCache(t)
	ctx := context.Background()

	_, slot := lists.Load(ctx, 2, "f")
	lists.Store(ctx, slot, []byte("team2"))

	server.Close()
	lists.InvalidateTeam(ctx, 1)
	if err := server.Restart(); err != nil {
		t.Fatal(err)
	}

	if data, _ := lists.Load(ctx, 2, "f"); string(data) != "team2" {
		t.Errorf("other team lost its entry: %q", data)
	}
}

type incrGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *incrGate) open() {
	g.once.Do(func() { close(g.release) })
}

func (g *incrGate) waitEntered(t *testing.T, who string) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not reach INCR", who)
	}
}

type retryScenario struct {
	lists   *TaskLists
	server  *miniredis.Miniredis
	gate    *incrGate
	readers sync.WaitGroup
}

func newRetryScenario(t *testing.T) *retryScenario {
	t.Helper()
	server := miniredis.RunT(t)
	scenario := &retryScenario{
		lists:  NewTaskLists(redis.NewClient(&redis.Options{Addr: server.Addr()})),
		server: server,
		gate:   &incrGate{entered: make(chan struct{}, 8), release: make(chan struct{})},
	}
	server.Server().SetPreHook(func(_ *miniserver.Peer, cmd string, _ ...string) bool {
		if strings.EqualFold(cmd, "INCR") {
			scenario.gate.entered <- struct{}{}
			<-scenario.gate.release
		}
		return false
	})

	t.Cleanup(scenario.readers.Wait)
	t.Cleanup(scenario.gate.open)
	return scenario
}

func (s *retryScenario) startReader() {
	s.readers.Add(1)
	go func() {
		defer s.readers.Done()
		s.lists.Load(context.Background(), 1, "f")
	}()
}

func TestSecondReaderStillSeesMarkWhileRetryIsInFlight(t *testing.T) {
	s := newRetryScenario(t)
	s.lists.markPending(1)

	s.startReader()
	s.gate.waitEntered(t, "first reader")

	s.startReader()
	s.gate.waitEntered(t, "second reader: the mark was dropped while the first INCR was in flight")
}

func TestNewFailureDuringRetryIsNotErased(t *testing.T) {
	s := newRetryScenario(t)
	s.lists.markPending(1)

	s.startReader()
	s.gate.waitEntered(t, "reader")

	s.lists.markPending(1)
	s.gate.open()
	s.readers.Wait()

	if _, stillPending := s.lists.pendingFailure(1); !stillPending {
		t.Fatal("a failure that happened during the retry was erased")
	}
}

func TestSuccessfulRetryClearsMarkAndMovesGenerationOnce(t *testing.T) {
	lists, server := newTestCache(t)
	lists.markPending(1)

	lists.Load(context.Background(), 1, "f")

	if _, stillPending := lists.pendingFailure(1); stillPending {
		t.Error("mark was not cleared after a successful retry")
	}
	if got, _ := server.Get(generationKey(1)); got != "1" {
		t.Errorf("generation = %q, want 1", got)
	}

	lists.Load(context.Background(), 1, "f")
	if got, _ := server.Get(generationKey(1)); got != "1" {
		t.Errorf("generation moved again without a new failure: %q", got)
	}
}

func TestClearPendingIgnoresOlderFailureNumber(t *testing.T) {
	lists, _ := newTestCache(t)

	lists.markPending(1)
	older, _ := lists.pendingFailure(1)
	lists.markPending(1)
	newer, _ := lists.pendingFailure(1)

	lists.clearPending(1, older)
	if _, ok := lists.pendingFailure(1); !ok {
		t.Fatal("older failure number cleared a newer mark")
	}
	lists.clearPending(1, newer)
	if _, ok := lists.pendingFailure(1); ok {
		t.Error("current failure number did not clear the mark")
	}
}
