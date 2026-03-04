package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestAllow_FirstCall(t *testing.T) {
	l := New(context.Background(), time.Minute)
	if !l.Allow("key1") {
		t.Error("first call should be allowed")
	}
}

func TestAllow_WithinTTL(t *testing.T) {
	l := New(context.Background(), time.Minute)
	l.Allow("key1")
	if l.Allow("key1") {
		t.Error("second call within TTL should be denied")
	}
}

func TestAllow_AfterTTL(t *testing.T) {
	l := New(context.Background(), 50*time.Millisecond)
	l.Allow("key1")
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("key1") {
		t.Error("call after TTL should be allowed")
	}
}

func TestAllow_DifferentKeys(t *testing.T) {
	l := New(context.Background(), time.Minute)
	if !l.Allow("key1") {
		t.Error("key1 first call should be allowed")
	}
	if !l.Allow("key2") {
		t.Error("key2 first call should be allowed")
	}
	if l.Allow("key1") {
		t.Error("key1 second call should be denied")
	}
}

func TestAllow_MaxEntries(t *testing.T) {
	l := New(context.Background(), time.Minute)
	// Fill to max
	for i := 0; i <= maxEntries; i++ {
		l.Allow(string(rune(i)))
	}
	l.mu.Lock()
	count := len(l.seen)
	l.mu.Unlock()
	if count > maxEntries {
		t.Errorf("expected at most %d entries, got %d", maxEntries, count)
	}
}

func TestCleanup_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	l := New(ctx, 10*time.Millisecond)
	l.Allow("key1")
	cancel()
	// Just verify no panic after cancel
	time.Sleep(30 * time.Millisecond)
}
