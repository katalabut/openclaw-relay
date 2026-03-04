package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookieName = "relay_session"
	sessionMaxAge     = 7 * 24 * 3600 // 7 days
)

// setSessionCookie sets an HMAC-signed session cookie with the user's email.
func setSessionCookie(w http.ResponseWriter, email, key string) {
	payload := hex.EncodeToString([]byte(email))
	sig := computeHMAC([]byte(email), key)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    payload + "." + sig,
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// getSessionEmail reads and verifies the session cookie, returning the email or "".
func getSessionEmail(r *http.Request, key string) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	emailBytes, err := hex.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	expected := computeHMAC(emailBytes, key)
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return ""
	}
	return string(emailBytes)
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
	})
}

func computeHMAC(data []byte, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
