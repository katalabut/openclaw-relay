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
	stateToken    string
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
	}
}

// OAuthConfig returns the oauth2 config for token refresh.
func (g *GoogleAuth) OAuthConfig() *oauth2.Config {
	return g.oauthCfg
}

func (g *GoogleAuth) generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	g.mu.Lock()
	g.stateToken = hex.EncodeToString(b)
	g.mu.Unlock()
	return g.stateToken
}

func (g *GoogleAuth) verifyState(state string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stateToken != "" && g.stateToken == state
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
	gt := g.store.GetGoogle()
	if gt != nil && gt.Email != "" {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body>
<h2>✅ Authenticated as %s</h2>
<p><a href="/auth/logout">Logout</a></p>
</body></html>`, gt.Email)
	} else {
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<h2>openclaw-relay</h2>
<p><a href="/auth/google/login"><button>Login with Google</button></a></p>
</body></html>`)
	}
}

func (g *GoogleAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := g.generateState()
	url := g.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (g *GoogleAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	if !g.verifyState(r.URL.Query().Get("state")) {
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
	if err := g.store.ClearGoogle(); err != nil {
		log.Printf("Clear token error: %v", err)
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// HandleAuthStatus returns auth status as JSON (for /api/auth/status).
func (g *GoogleAuth) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	gt := g.store.GetGoogle()
	w.Header().Set("Content-Type", "application/json")
	if gt == nil {
		json.NewEncoder(w).Encode(map[string]any{"google": map[string]any{"authenticated": false}})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"google": map[string]any{
			"authenticated": true,
			"email":         gt.Email,
			"expires_at":    gt.Expiry,
		},
	})
}
