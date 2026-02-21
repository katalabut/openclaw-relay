package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/tokens"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	googleoauth2api "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
)

var (
	oauthScopes = []string{
		"https://www.googleapis.com/auth/gmail.modify",
		"https://www.googleapis.com/auth/calendar.readonly",
		"https://www.googleapis.com/auth/userinfo.email",
	}
)

// GoogleAuth handles OAuth web flow.
type GoogleAuth struct {
	oauthCfg      *oauth2.Config
	allowedEmails map[string]bool
	store         *tokens.Store
	mu            sync.Mutex
	stateToEmail  map[string]string
}

func NewGoogleAuth(cfg *config.GoogleConfig, store *tokens.Store) *GoogleAuth {
	allowed := make(map[string]bool, len(cfg.AllowedEmails))
	for _, e := range cfg.AllowedEmails {
		allowed[e] = true
	}
	return &GoogleAuth{
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       oauthScopes,
			Endpoint:     google.Endpoint,
		},
		allowedEmails: allowed,
		store:         store,
		stateToEmail:  map[string]string{},
	}
}

// OAuthConfig returns the oauth2 config for token refresh.
func (g *GoogleAuth) OAuthConfig() *oauth2.Config {
	return g.oauthCfg
}

func (g *GoogleAuth) generateState(requestedEmail ...string) string {
	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)
	email := ""
	if len(requestedEmail) > 0 {
		email = requestedEmail[0]
	}
	g.mu.Lock()
	g.stateToEmail[state] = email
	g.mu.Unlock()
	return state
}

func (g *GoogleAuth) consumeState(state string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	email, ok := g.stateToEmail[state]
	if ok {
		delete(g.stateToEmail, state)
	}
	return email, ok
}

// verifyState keeps backward-compatible behavior for tests.
func (g *GoogleAuth) verifyState(state string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.stateToEmail[state]
	return ok
}

// RegisterRoutes adds OAuth routes to the mux.
func (g *GoogleAuth) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", g.handleRoot)
	mux.HandleFunc("/auth/google/login", g.handleLogin)
	mux.HandleFunc("/auth/google/callback", g.handleCallback)
	mux.HandleFunc("/auth/logout", g.handleLogout)
}

func (g *GoogleAuth) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	accounts := g.store.ListGoogle()
	fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>openclaw-relay</h2>`)
	if len(g.allowedEmails) == 0 {
		fmt.Fprint(w, `<p>No allowed emails configured.</p>`)
	} else {
		fmt.Fprint(w, `<h3>Google accounts</h3><ul>`)
		for email := range g.allowedEmails {
			if _, ok := accounts[email]; ok {
				fmt.Fprintf(w, `<li>✅ %s — <a href="/auth/logout?account=%s">Logout</a></li>`, email, email)
			} else {
				fmt.Fprintf(w, `<li>⬜ %s — <a href="/auth/google/login?account=%s">Login</a></li>`, email, email)
			}
		}
		fmt.Fprint(w, `</ul>`)
	}
	fmt.Fprint(w, `</body></html>`)
}

func (g *GoogleAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	account := r.URL.Query().Get("account")
	if account != "" && !g.allowedEmails[account] {
		http.Error(w, "account is not allowed", http.StatusForbidden)
		return
	}
	state := g.generateState(account)
	url := g.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (g *GoogleAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	expectedEmail, ok := g.consumeState(r.URL.Query().Get("state"))
	if !ok {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	token, err := g.oauthCfg.Exchange(ctx, code)
	if err != nil {
		log.Printf("OAuth exchange error: %v", err)
		http.Error(w, "OAuth exchange failed", http.StatusInternalServerError)
		return
	}

	// Get user email
	svc, err := googleoauth2api.NewService(ctx, option.WithTokenSource(g.oauthCfg.TokenSource(ctx, token)))
	if err != nil {
		log.Printf("OAuth2 service error: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	userInfo, err := svc.Userinfo.Get().Do()
	if err != nil {
		log.Printf("Userinfo error: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	email := userInfo.Email
	if expectedEmail != "" && email != expectedEmail {
		log.Printf("OAuth email mismatch: expected=%s got=%s", expectedEmail, email)
		http.Error(w, "authenticated with different account", http.StatusForbidden)
		return
	}
	if !g.allowedEmails[email] {
		log.Printf("Rejected email: %s", email)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `<!DOCTYPE html><html><body>
<h2>⛔ Access Denied</h2>
<p>Email %s is not in the allowed list.</p>
<p><a href="/">Back</a></p>
</body></html>`, email)
		return
	}

	if err := g.store.SaveGoogle(token, email); err != nil {
		log.Printf("Token save error: %v", err)
		http.Error(w, "Failed to save token", http.StatusInternalServerError)
		return
	}

	log.Printf("Google OAuth success for %s", email)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (g *GoogleAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	account := r.URL.Query().Get("account")
	if err := g.store.ClearGoogle(account); err != nil {
		log.Printf("Clear token error: %v", err)
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// HandleAuthStatus returns auth status as JSON (for /api/auth/status).
func (g *GoogleAuth) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	accounts := g.store.ListGoogle()
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"google": map[string]any{"authenticated": len(accounts) > 0}}
	googleMap := resp["google"].(map[string]any)
	list := make([]map[string]any, 0, len(accounts))
	for _, gt := range accounts {
		list = append(list, map[string]any{
			"email":      gt.Email,
			"expires_at": gt.Expiry,
		})
	}
	googleMap["accounts"] = list
	json.NewEncoder(w).Encode(resp)
}
