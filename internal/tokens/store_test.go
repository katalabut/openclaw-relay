package tokens

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	s, err := NewStore(fp, key)
	if err != nil {
		t.Fatal(err)
	}

	tok := &oauth2.Token{
		AccessToken:  "access123",
		RefreshToken: "refresh456",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}
	if err := s.SaveGoogle(tok, "test@example.com"); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(fp, key)
	if err != nil {
		t.Fatal(err)
	}
	g := s2.GetGoogle()
	if g == nil {
		t.Fatal("expected google token")
	}
	if g.AccessToken != "access123" || g.Email != "test@example.com" {
		t.Fatalf("unexpected token: %+v", g)
	}
}

func TestStoreInvalidKey(t *testing.T) {
	_, err := NewStore("/tmp/test.enc", "short")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestStoreClearGoogle(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	s, err := NewStore(fp, key)
	if err != nil {
		t.Fatal(err)
	}
	tok := &oauth2.Token{AccessToken: "x", RefreshToken: "y", Expiry: time.Now().Add(time.Hour)}
	s.SaveGoogle(tok, "a@b.com")
	s.ClearGoogle()

	s2, err := NewStore(fp, key)
	if err != nil {
		t.Fatal(err)
	}
	if s2.GetGoogle() != nil {
		t.Fatal("expected nil after clear")
	}
}

func TestStoreWrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key1 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key2 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	s, _ := NewStore(fp, key1)
	tok := &oauth2.Token{AccessToken: "x", RefreshToken: "y", Expiry: time.Now().Add(time.Hour)}
	s.SaveGoogle(tok, "a@b.com")

	_, err := NewStore(fp, key2)
	if err == nil {
		t.Fatal("expected decrypt error with wrong key")
	}
	_ = os.Remove(fp)
}

func TestGetGoogleOAuth2Token_Valid(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	s, _ := NewStore(fp, key)
	tok := &oauth2.Token{AccessToken: "acc", RefreshToken: "ref", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}
	s.SaveGoogle(tok, "a@b.com")

	ot := s.GetGoogleOAuth2Token()
	if ot == nil {
		t.Fatal("expected token")
	}
	if ot.AccessToken != "acc" || ot.RefreshToken != "ref" {
		t.Errorf("unexpected: %+v", ot)
	}
}

func TestGetGoogleOAuth2Token_Empty(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	s, _ := NewStore(fp, key)
	if s.GetGoogleOAuth2Token() != nil {
		t.Error("expected nil for empty store")
	}
}

func TestUpdateGoogleAccessToken(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	s, _ := NewStore(fp, key)
	tok := &oauth2.Token{AccessToken: "old", RefreshToken: "ref", Expiry: time.Now().Add(time.Hour)}
	s.SaveGoogle(tok, "a@b.com")

	newTok := &oauth2.Token{AccessToken: "new", Expiry: time.Now().Add(2 * time.Hour)}
	if err := s.UpdateGoogleAccessToken(newTok); err != nil {
		t.Fatal(err)
	}

	g := s.GetGoogle()
	if g.AccessToken != "new" {
		t.Errorf("expected new, got %s", g.AccessToken)
	}
	if g.RefreshToken != "ref" {
		t.Errorf("refresh token should be preserved, got %s", g.RefreshToken)
	}
}

func TestUpdateGoogleAccessToken_NoToken(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "tokens.json.enc")
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	s, _ := NewStore(fp, key)
	err := s.UpdateGoogleAccessToken(&oauth2.Token{AccessToken: "x"})
	if err == nil {
		t.Error("expected error when no token exists")
	}
}
