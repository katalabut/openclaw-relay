package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testSessionKey = "0123456789abcdef0123456789abcdef"

func TestSetAndGetSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	setSessionCookie(rec, "user@example.com", testSessionKey)

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie to be set")
	}
	c := cookies[0]
	if c.Name != sessionCookieName {
		t.Errorf("expected cookie name %s, got %s", sessionCookieName, c.Name)
	}
	if !c.HttpOnly {
		t.Error("expected HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Error("expected SameSite=Lax")
	}

	// Read it back
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	email := getSessionEmail(req, testSessionKey)
	if email != "user@example.com" {
		t.Errorf("expected user@example.com, got %q", email)
	}
}

func TestGetSessionEmail_NoCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if email := getSessionEmail(req, testSessionKey); email != "" {
		t.Errorf("expected empty, got %q", email)
	}
}

func TestGetSessionEmail_InvalidSignature(t *testing.T) {
	rec := httptest.NewRecorder()
	setSessionCookie(rec, "user@example.com", testSessionKey)

	c := rec.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)

	// Different key → invalid signature
	if email := getSessionEmail(req, "different-key-different-key-1234"); email != "" {
		t.Errorf("expected empty for wrong key, got %q", email)
	}
}

func TestGetSessionEmail_MalformedCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "no-dot-here"})
	if email := getSessionEmail(req, testSessionKey); email != "" {
		t.Errorf("expected empty for malformed cookie, got %q", email)
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "zzzz.badsig"})
	if email := getSessionEmail(req2, testSessionKey); email != "" {
		t.Errorf("expected empty for bad hex, got %q", email)
	}
}

func TestClearSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	clearSessionCookie(rec)

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie to be set for clearing")
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("expected MaxAge=-1, got %d", cookies[0].MaxAge)
	}
}
