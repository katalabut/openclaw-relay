package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/tokens"
	"golang.org/x/oauth2"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestGoogleAuth(t *testing.T) (*GoogleAuth, *tokens.Store) {
	t.Helper()
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	store, err := tokens.NewStore(fp, testKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.GoogleConfig{
		ClientID:      "test-client-id",
		ClientSecret:  "test-secret",
		RedirectURL:   "http://localhost/auth/google/callback",
		AllowedEmails: []string{"test@example.com"},
	}
	ga := NewGoogleAuth(cfg, store)
	return ga, store
}

func TestGenerateState(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	s1 := ga.generateState()
	s2 := ga.generateState()
	if s1 == "" || s2 == "" {
		t.Error("state should not be empty")
	}
	if s1 == s2 {
		t.Error("states should be unique")
	}
	if len(s1) != 32 {
		t.Errorf("expected 32 hex chars, got %d", len(s1))
	}
}

func TestHandleRoot_NotAuthenticated(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body == "" {
		t.Error("expected HTML body")
	}
}

func TestHandleRoot_Authenticated(t *testing.T) {
	ga, store := newTestGoogleAuth(t)
	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	store.SaveGoogle(tok, "test@example.com")

	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleAuthStatus_NotAuth(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	ga.HandleAuthStatus(rec, req)

	var resp map[string]map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["google"]["authenticated"] != false {
		t.Error("expected not authenticated")
	}
}

func TestHandleAuthStatus_Auth(t *testing.T) {
	ga, store := newTestGoogleAuth(t)
	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	store.SaveGoogle(tok, "test@example.com")

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	ga.HandleAuthStatus(rec, req)

	var resp map[string]map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["google"]["authenticated"] != true {
		t.Error("expected authenticated")
	}
	if resp["google"]["email"] != "test@example.com" {
		t.Errorf("expected test@example.com, got %v", resp["google"]["email"])
	}
}

func TestOAuthConfig(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	cfg := ga.OAuthConfig()
	if cfg.ClientID != "test-client-id" {
		t.Errorf("expected test-client-id, got %s", cfg.ClientID)
	}
}

func TestHandleLogin(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/auth/google/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected 307, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Error("expected redirect URL")
	}
}

func TestHandleCallback_InvalidState(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/auth/google/callback?state=invalid&code=test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCallback_MissingCode(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	state := ga.generateState()
	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/auth/google/callback?state="+state, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCallback_ExchangeError(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	state := ga.generateState()
	// Override endpoint to a bad URL to force exchange error
	ga.oauthCfg.Endpoint.TokenURL = "http://localhost:1/bad"
	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/auth/google/callback?state="+state+"&code=badcode", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleLogout(t *testing.T) {
	ga, store := newTestGoogleAuth(t)
	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	store.SaveGoogle(tok, "test@example.com")

	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/auth/logout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected 307, got %d", rec.Code)
	}
	if store.GetGoogle() != nil {
		t.Error("expected token cleared after logout")
	}
}

func TestHandleRoot_NotFound(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	mux := http.NewServeMux()
	ga.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestVerifyState(t *testing.T) {
	ga, _ := newTestGoogleAuth(t)
	state := ga.generateState()
	if !ga.verifyState(state) {
		t.Error("should verify valid state")
	}
	if ga.verifyState("invalid") {
		t.Error("should reject invalid state")
	}
}
