// Предохранитель с тремя состояниями. Closed: вызовы идут, подряд идущие сбои
// считаются, при достижении порога цепь размыкается. Open: вызовы отклоняются
// сразу, без обращения к сервису, пока не пройдёт пауза. Half-open: после паузы
// пропускается ровно один пробный вызов; успех замыкает цепь, сбой размыкает её
// заново с новой паузой, нейтральный исход (например, отмена запроса клиентом)
// ничего не доказывает и освобождает место для следующей пробы.
//
// Каждое состояние имеет номер поколения: результат вызова, начатого в
// предыдущем состоянии, не меняет текущее.

package breaker

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

var ErrOpen = errors.New("circuit breaker is open")

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	return [...]string{"closed", "open", "half-open"}[s]
}

type Outcome int

const (
	Success Outcome = iota
	Failure
	Ignore
)

type Config struct {
	Threshold int
	Cooldown  time.Duration
	Logger    *slog.Logger
	Now       func() time.Time
}

type Breaker struct {
	cfg Config

	mu         sync.Mutex
	state      State
	generation uint64
	failures   int
	openedAt   time.Time
	probing    bool
}

func New(cfg Config) *Breaker {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Breaker{cfg: cfg}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) Allow() (func(Outcome), error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Open:
		if b.cfg.Now().Sub(b.openedAt) < b.cfg.Cooldown {
			return nil, ErrOpen
		}
		b.transition(HalfOpen)
		b.probing = true
		return b.finisher(true), nil
	case HalfOpen:
		if b.probing {
			return nil, ErrOpen
		}
		b.probing = true
		return b.finisher(true), nil
	default:
		return b.finisher(false), nil
	}
}

func (b *Breaker) finisher(probe bool) func(Outcome) {
	generation := b.generation
	return func(outcome Outcome) {
		b.mu.Lock()
		defer b.mu.Unlock()

		if generation != b.generation {
			return
		}
		if probe {
			b.finishProbe(outcome)
			return
		}
		switch outcome {
		case Success:
			b.failures = 0
		case Failure:
			b.failures++
			if b.failures >= b.cfg.Threshold {
				b.transition(Open)
			}
		}
	}
}

func (b *Breaker) finishProbe(outcome Outcome) {
	switch outcome {
	case Success:
		b.transition(Closed)
	case Failure:
		b.transition(Open)
	default:
		b.probing = false
	}
}

func (b *Breaker) transition(to State) {
	from := b.state
	b.state = to
	b.generation++
	b.failures = 0
	b.probing = false
	if to == Open {
		b.openedAt = b.cfg.Now()
	}
	b.cfg.Logger.Warn("circuit breaker state changed", "from", from.String(), "to", to.String())
}
