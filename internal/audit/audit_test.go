package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewLogger_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "audit.log")
	l, err := NewLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
}

func TestNewLogger_MkdirError(t *testing.T) {
	// Try creating under /dev/null which isn't a directory
	_, err := NewLogger("/dev/null/sub/audit.log")
	if err == nil {
		t.Error("expected error")
	}
}

func TestLog_WritesJSONEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	l.Log(Entry{
		Timestamp: "2025-01-01T00:00:00Z",
		Method:    "GET",
		Path:      "/test",
		Status:    200,
		SourceIP:  "127.0.0.1",
		LatencyMs: 5,
	})

	data, _ := os.ReadFile(path)
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if e.Method != "GET" || e.Path != "/test" || e.Status != 200 {
		t.Errorf("unexpected entry: %+v", e)
	}
}

func TestMiddleware_WrapsHandler(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, _ := NewLogger(path)
	defer l.Close()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := Middleware(l, inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	data, _ := os.ReadFile(path)
	var e Entry
	json.Unmarshal(data, &e)
	if e.Path != "/test" || e.Status != 200 {
		t.Errorf("unexpected audit entry: %+v", e)
	}
}

func TestMiddleware_CapturingStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, _ := NewLogger(path)
	defer l.Close()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := Middleware(l, inner)
	req := httptest.NewRequest("GET", "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	data, _ := os.ReadFile(path)
	var e Entry
	json.Unmarshal(data, &e)
	if e.Status != 404 {
		t.Errorf("expected status 404, got %d", e.Status)
	}
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	ip := extractClientIP(req)
	if ip != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", ip)
	}
}

func TestExtractClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	ip := extractClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", ip)
	}
}

func TestExtractClientIP_RemoteAddr_NoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1"
	req.Header.Del("X-Forwarded-For")
	ip := extractClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", ip)
	}
}

func TestMiddleware_ExtractsIP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, _ := NewLogger(path)
	defer l.Close()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(l, inner)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "5.6.7.8")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	data, _ := os.ReadFile(path)
	var e Entry
	json.Unmarshal(data, &e)
	if e.SourceIP != "5.6.7.8" {
		t.Errorf("expected 5.6.7.8, got %s", e.SourceIP)
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}
}
