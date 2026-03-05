package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://example.com/", "tok", "agent1", "")
	if c.URL != "http://example.com" {
		t.Errorf("URL not trimmed: %s", c.URL)
	}
	if c.Token != "tok" || c.AgentID != "agent1" {
		t.Error("fields not set correctly")
	}
	if c.Model != "" {
		t.Errorf("expected empty model, got %s", c.Model)
	}
}

func TestNewClient_CustomModel(t *testing.T) {
	c := NewClient("http://example.com", "tok", "agent1", "openai/gpt-4")
	if c.Model != "openai/gpt-4" {
		t.Errorf("expected custom model, got %s", c.Model)
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

	c := NewClient(srv.URL, "tok", "agent1", "")
	err := c.CreateOneShotJob("test", "hello", 120, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateOneShotJob_HTTPError_4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1", "")
	err := c.CreateOneShotJob("test", "hello", 120, 2)
	if err == nil {
		t.Fatal("expected error")
	}
	// 4xx errors should not be retried, so error should not mention "after X attempts"
	if _, ok := err.(*clientError); !ok {
		t.Errorf("expected clientError, got %T: %v", err, err)
	}
}

func TestCreateOneShotJob_HTTPError_5xx_Retries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1", "")
	// Override HTTP client timeout for faster tests
	c.HTTP.Timeout = 0
	err := c.CreateOneShotJob("test", "hello", 120, 0)
	// With 3 retries (4 total attempts) and server failing 3 times, 4th should succeed
	if err != nil {
		t.Fatalf("expected success on 4th attempt, got: %v", err)
	}
	if attempts != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts)
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

	c := NewClient(srv.URL, "tok", "agent1", "")
	c.CreateOneShotJob("test", "msg", 120, 2)
}

func TestCreateOneShotJob_NotConfigured(t *testing.T) {
	c := NewClient("", "", "agent1", "")
	err := c.CreateOneShotJob("test", "msg", 120, 2)
	if err != nil {
		t.Fatalf("empty config should not error: %v", err)
	}
}

func TestCreateOneShotJob_NoModel_OmittedFromPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]json.RawMessage
		json.Unmarshal(body, &req)
		var args map[string]json.RawMessage
		json.Unmarshal(req["args"], &args)
		var job map[string]json.RawMessage
		json.Unmarshal(args["job"], &job)
		var payload map[string]interface{}
		json.Unmarshal(job["payload"], &payload)
		if _, exists := payload["model"]; exists {
			t.Errorf("expected model to be omitted, got %v", payload["model"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1", "")
	c.CreateOneShotJob("test", "msg", 120, 2)
}

func TestCreateOneShotJob_CustomModel_InPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]json.RawMessage
		json.Unmarshal(body, &req)
		var args map[string]json.RawMessage
		json.Unmarshal(req["args"], &args)
		var job map[string]json.RawMessage
		json.Unmarshal(args["job"], &job)
		var payload map[string]interface{}
		json.Unmarshal(job["payload"], &payload)
		if payload["model"] != "my-model" {
			t.Errorf("expected model my-model, got %v", payload["model"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", "agent1", "my-model")
	c.CreateOneShotJob("test", "msg", 120, 2)
}
