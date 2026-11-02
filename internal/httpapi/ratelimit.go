package httpapi

import (
	"sync"
	"time"
)

// limiter counts failures per key in a fixed window, in memory. Good enough for
// one instance; a multi-instance hosted deploy would move this to Postgres.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*limitEntry
	now    func() time.Time
}

type limitEntry struct {
	count int
	start time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string]*limitEntry{}, now: time.Now}
}

func (l *limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.hits[key]
	return e != nil && l.now().Sub(e.start) < l.window && e.count >= l.max
}

func (l *limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if e := l.hits[key]; e != nil && now.Sub(e.start) < l.window {
		e.count++
		return
	}
	if len(l.hits) >= 10_000 {
		for k, e := range l.hits {
			if now.Sub(e.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	l.hits[key] = &limitEntry{count: 1, start: now}
}

func (l *limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
