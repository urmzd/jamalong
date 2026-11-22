package server

import (
	"sync"
	"time"
)

// limiter allows each key a fixed number of events per window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	seen   map[string]*bucket
}

type bucket struct {
	start time.Time
	count int
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{max: limit, window: window, seen: map[string]*bucket{}}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.seen[key]
	if !ok || now.Sub(b.start) >= l.window {
		l.seen[key] = &bucket{start: now, count: 1}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

// prune forgets keys whose window has passed.
func (l *limiter) prune(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.seen {
		if now.Sub(b.start) >= l.window {
			delete(l.seen, k)
		}
	}
}

// tokens is a token bucket for one connection. It is used from a single
// goroutine.
type tokens struct {
	level float64
	burst float64
	rate  float64 // per second
	last  time.Time
}

func (t *tokens) take(now time.Time) bool {
	if !t.last.IsZero() {
		t.level += now.Sub(t.last).Seconds() * t.rate
		if t.level > t.burst {
			t.level = t.burst
		}
	}
	t.last = now
	if t.level < 1 {
		return false
	}
	t.level--
	return true
}
