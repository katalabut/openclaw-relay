package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func New(ttl time.Duration) *Limiter {
	l := &Limiter{seen: make(map[string]time.Time), ttl: ttl}
	go l.cleanup()
	return l
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.seen[key]; ok && time.Since(last) < l.ttl {
		return false
	}
	l.seen[key] = time.Now()
	return true
}

func (l *Limiter) cleanup() {
	for {
		time.Sleep(l.ttl * 2)
		l.mu.Lock()
		for k, v := range l.seen {
			if time.Since(v) > l.ttl {
				delete(l.seen, k)
			}
		}
		l.mu.Unlock()
	}
}
