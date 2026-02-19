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
	defer l.file.Close()
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
	defer l.file.Close()

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
	defer l.file.Close()

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
	defer l.file.Close()

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
