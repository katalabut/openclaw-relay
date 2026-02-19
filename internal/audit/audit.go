package audit

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	Timestamp string `json:"timestamp"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	SourceIP  string `json:"source_ip"`
	LatencyMs int64  `json:"latency_ms"`
}

type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func NewLogger(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

func (l *Logger) Log(e Entry) {
	data, err := json.Marshal(e)
	if err != nil {
		log.Printf("audit marshal error: %v", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file.Write(append(data, '\n'))
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func Middleware(logger *Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		logger.Log(Entry{
			Timestamp: start.UTC().Format(time.RFC3339),
			Method:    r.Method,
			Path:      r.URL.Path,
			Status:    rw.status,
			SourceIP:  r.RemoteAddr,
			LatencyMs: time.Since(start).Milliseconds(),
		})
	})
}
