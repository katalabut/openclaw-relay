package ratelimit

import (
	"context"
	"sync"
	"time"
)

const maxEntries = 10000

type Limiter struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func New(ctx context.Context, ttl time.Duration) *Limiter {
	l := &Limiter{seen: make(map[string]time.Time), ttl: ttl}
	go l.cleanup(ctx)
	return l
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.seen[key]; ok && time.Since(last) < l.ttl {
		return false
	}
	l.seen[key] = time.Now()
	if len(l.seen) > maxEntries {
		l.evictOldest()
	}
	return true
}

func (l *Limiter) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range l.seen {
		if oldestKey == "" || v.Before(oldestTime) {
			oldestKey = k
			oldestTime = v
		}
	}
	if oldestKey != "" {
		delete(l.seen, oldestKey)
	}
}

func (l *Limiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(l.ttl * 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			for k, v := range l.seen {
				if time.Since(v) > l.ttl {
					delete(l.seen, k)
				}
			}
			l.mu.Unlock()
		}
	}
}
