package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://example.com/", "tok", "agent1")
	if c.URL != "http://example.com" {
		t.Errorf("URL not trimmed: %s", c.URL)
	}
	if c.Token != "tok" || c.AgentID != "agent1" {
		t.Error("fields not set correctly")
	}
}

func TestCreateOneShotJob_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing auth header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1")
	err := c.CreateOneShotJob("test", "hello", 120, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateOneShotJob_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1")
	err := c.CreateOneShotJob("test", "hello", 120, 2)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateOneShotJob_Payload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["tool"] != "cron" {
			t.Errorf("expected tool=cron, got %v", req["tool"])
		}
		if req["sessionKey"] != "agent:agent1:main" {
			t.Errorf("unexpected sessionKey: %v", req["sessionKey"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1")
	c.CreateOneShotJob("test", "msg", 120, 2)
}

func TestCreateOneShotJob_NotConfigured(t *testing.T) {
	c := NewClient("", "", "agent1")
	err := c.CreateOneShotJob("test", "msg", 120, 2)
	if err != nil {
		t.Fatalf("empty config should not error: %v", err)
	}
}
