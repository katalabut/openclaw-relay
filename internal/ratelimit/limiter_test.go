package ratelimit

import (
	"testing"
	"time"
)

func TestAllow_FirstCall(t *testing.T) {
	l := New(time.Minute)
	if !l.Allow("key1") {
		t.Error("first call should be allowed")
	}
}

func TestAllow_WithinTTL(t *testing.T) {
	l := New(time.Minute)
	l.Allow("key1")
	if l.Allow("key1") {
		t.Error("second call within TTL should be denied")
	}
}

func TestAllow_AfterTTL(t *testing.T) {
	l := New(50 * time.Millisecond)
	l.Allow("key1")
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("key1") {
		t.Error("call after TTL should be allowed")
	}
}

func TestAllow_DifferentKeys(t *testing.T) {
	l := New(time.Minute)
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
