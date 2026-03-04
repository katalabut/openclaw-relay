package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"sync"
	"time"

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

	stateTTL = 10 * time.Minute
)

type stateEntry struct {
	email     string
	createdAt time.Time
}

// GoogleAuth handles OAuth web flow.
type GoogleAuth struct {
	oauthCfg      *oauth2.Config
	allowedEmails map[string]bool
	store         *tokens.Store
	encKey        string
	appCfg        *config.Config
	mu            sync.Mutex
	stateToEmail  map[string]stateEntry
}

func NewGoogleAuth(ctx context.Context, cfg *config.GoogleConfig, store *tokens.Store, encKey string, appCfg *config.Config) *GoogleAuth {
	allowed := make(map[string]bool, len(cfg.AllowedEmails))
	for _, e := range cfg.AllowedEmails {
		allowed[e] = true
	}
	ga := &GoogleAuth{
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       oauthScopes,
			Endpoint:     google.Endpoint,
		},
		allowedEmails: allowed,
		store:         store,
		encKey:        encKey,
		appCfg:        appCfg,
		stateToEmail:  map[string]stateEntry{},
	}
	go ga.cleanupStates(ctx)
	return ga
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
	g.stateToEmail[state] = stateEntry{email: email, createdAt: time.Now()}
	g.mu.Unlock()
	return state
}

func (g *GoogleAuth) consumeState(state string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.stateToEmail[state]
	if ok {
		delete(g.stateToEmail, state)
		if time.Since(entry.createdAt) > stateTTL {
			return "", false
		}
	}
	return entry.email, ok
}

// verifyState keeps backward-compatible behavior for tests.
func (g *GoogleAuth) verifyState(state string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.stateToEmail[state]
	return ok
}

func (g *GoogleAuth) cleanupStates(ctx context.Context) {
	ticker := time.NewTicker(stateTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.mu.Lock()
			now := time.Now()
			for k, entry := range g.stateToEmail {
				if now.Sub(entry.createdAt) > stateTTL {
					delete(g.stateToEmail, k)
				}
			}
			g.mu.Unlock()
		}
	}
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

	sessionEmail := getSessionEmail(r, g.encKey)

	// No valid session → login page
	if sessionEmail == "" {
		g.renderLoginPage(w)
		return
	}
	// Session exists but email not allowed → clear and show login
	if !g.allowedEmails[sessionEmail] {
		clearSessionCookie(w)
		g.renderLoginPage(w)
		return
	}

	g.renderDashboard(w, sessionEmail)
}

func (g *GoogleAuth) renderLoginPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>openclaw-relay</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0d1117;color:#c9d1d9;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;min-height:100vh;display:flex;align-items:center;justify-content:center}
.card{background:#161b22;border:1px solid #30363d;border-radius:12px;padding:48px;text-align:center;max-width:400px;width:90%}
h1{font-size:24px;font-weight:600;margin-bottom:8px;color:#f0f6fc}
.sub{color:#8b949e;font-size:14px;margin-bottom:32px}
.btn{display:inline-flex;align-items:center;gap:8px;background:#238636;color:#fff;border:none;border-radius:6px;padding:12px 24px;font-size:16px;font-weight:500;cursor:pointer;text-decoration:none;transition:background .15s}
.btn:hover{background:#2ea043}
.btn svg{width:20px;height:20px}
</style></head><body>
<div class="card">
<h1>openclaw-relay</h1>
<p class="sub">Sign in to access the dashboard</p>
<a class="btn" href="/auth/google/login">
<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12.545 10.239v3.821h5.445c-.712 2.315-2.647 3.972-5.445 3.972a6.033 6.033 0 110-12.064c1.498 0 2.866.549 3.921 1.453l2.814-2.814A9.969 9.969 0 0012.545 2C7.021 2 2.543 6.477 2.543 12s4.478 10 10.002 10c8.396 0 10.249-7.85 9.426-11.748l-9.426-.013z"/></svg>
Sign in with Google
</a>
</div>
</body></html>`)
}

func (g *GoogleAuth) renderDashboard(w http.ResponseWriter, sessionEmail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	accounts := g.store.ListGoogle()

	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>openclaw-relay — Dashboard</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0d1117;color:#c9d1d9;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;min-height:100vh}
.header{background:#161b22;border-bottom:1px solid #30363d;padding:16px 24px;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:12px}
.header h1{font-size:20px;font-weight:600;color:#f0f6fc}
.header-right{display:flex;align-items:center;gap:16px;font-size:14px}
.header-right .email{color:#8b949e}
.btn-logout{color:#f85149;text-decoration:none;font-size:14px;padding:6px 12px;border:1px solid #f8514933;border-radius:6px;transition:background .15s}
.btn-logout:hover{background:#f8514922}
.container{max-width:960px;margin:0 auto;padding:24px}
.section{margin-bottom:24px}
.section h2{font-size:16px;font-weight:600;color:#f0f6fc;margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid #21262d}
.card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:16px;margin-bottom:8px}
.card-row{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:8px}
.badge{display:inline-block;padding:2px 8px;border-radius:12px;font-size:12px;font-weight:500}
.badge-ok{background:#23863633;color:#3fb950;border:1px solid #23863666}
.badge-off{background:#30363d;color:#8b949e;border:1px solid #30363d}
.badge-warn{background:#d2992233;color:#d29922;border:1px solid #d2992266}
.btn-sm{font-size:13px;padding:4px 12px;border-radius:6px;text-decoration:none;border:1px solid #30363d;color:#c9d1d9;transition:background .15s}
.btn-sm:hover{background:#30363d}
.info{font-size:13px;color:#8b949e}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:8px}
.link-card{display:block;background:#161b22;border:1px solid #30363d;border-radius:8px;padding:12px 16px;color:#58a6ff;text-decoration:none;font-size:14px;transition:border-color .15s}
.link-card:hover{border-color:#58a6ff}
.link-card .desc{color:#8b949e;font-size:12px;margin-top:4px}
</style></head><body>
<div class="header">
<h1>openclaw-relay</h1>
<div class="header-right">`)
	fmt.Fprintf(w, `<span class="email">%s</span>`, html.EscapeString(sessionEmail))
	fmt.Fprint(w, `<a class="btn-logout" href="/auth/logout">Logout</a>
</div></div>
<div class="container">`)

	// Google Accounts section
	fmt.Fprint(w, `<div class="section"><h2>Google Accounts</h2>`)
	for email := range g.allowedEmails {
		fmt.Fprint(w, `<div class="card"><div class="card-row">`)
		if _, ok := accounts[email]; ok {
			fmt.Fprintf(w, `<div><span class="badge badge-ok">connected</span> <span>%s</span></div>`, html.EscapeString(email))
			fmt.Fprintf(w, `<a class="btn-sm" href="/auth/logout?account=%s">Disconnect</a>`, html.EscapeString(email))
		} else {
			fmt.Fprintf(w, `<div><span class="badge badge-off">not connected</span> <span>%s</span></div>`, html.EscapeString(email))
			fmt.Fprintf(w, `<a class="btn-sm" href="/auth/google/login?account=%s">Connect</a>`, html.EscapeString(email))
		}
		fmt.Fprint(w, `</div></div>`)
	}
	fmt.Fprint(w, `</div>`)

	// Integrations section
	fmt.Fprint(w, `<div class="section"><h2>Integrations</h2>`)
	if g.appCfg != nil {
		// Gmail
		fmt.Fprint(w, `<div class="card"><div class="card-row"><div><strong>Gmail</strong></div>`)
		if g.appCfg.Gmail.Enabled {
			accs := g.appCfg.Gmail.ResolvedAccounts()
			fmt.Fprintf(w, `<span class="badge badge-ok">enabled</span></div>`)
			pollInterval := g.appCfg.Gmail.PollInterval
			if pollInterval == "" {
				pollInterval = "default"
			}
			fmt.Fprintf(w, `<div class="info">Poll: %s · %d account(s)</div>`, html.EscapeString(pollInterval), len(accs))
		} else {
			fmt.Fprint(w, `<span class="badge badge-off">disabled</span></div>`)
		}
		fmt.Fprint(w, `</div>`)

		// Trello
		fmt.Fprint(w, `<div class="card"><div class="card-row"><div><strong>Trello</strong></div>`)
		if g.appCfg.Trello.Secret != "" {
			fmt.Fprintf(w, `<span class="badge badge-ok">configured</span></div>`)
			fmt.Fprintf(w, `<div class="info">%d rule(s)</div>`, len(g.appCfg.Trello.Rules))
		} else {
			fmt.Fprint(w, `<span class="badge badge-off">not configured</span></div>`)
		}
		fmt.Fprint(w, `</div>`)

		// GitHub
		fmt.Fprint(w, `<div class="card"><div class="card-row"><div><strong>GitHub</strong></div>`)
		if g.appCfg.GitHub.Secret != "" {
			fmt.Fprint(w, `<span class="badge badge-ok">configured</span></div>`)
		} else {
			fmt.Fprint(w, `<span class="badge badge-off">not configured</span></div>`)
		}
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `</div>`)

	// Quick Links
	fmt.Fprint(w, `<div class="section"><h2>Quick Links</h2><div class="grid">
<a class="link-card" href="/health">/health<div class="desc">Service health check</div></a>
<a class="link-card" href="/api/status">/api/status<div class="desc">API status endpoint</div></a>
</div></div>`)

	fmt.Fprint(w, `</div></body></html>`)
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

	setSessionCookie(w, email, g.encKey)
	log.Printf("Google OAuth success for %s", email)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (g *GoogleAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	account := r.URL.Query().Get("account")
	if account != "" {
		// Disconnecting a specific Google account (not ending user session)
		if err := g.store.ClearGoogle(account); err != nil {
			log.Printf("Clear token error: %v", err)
		}
	} else {
		// Full logout — clear session cookie
		clearSessionCookie(w)
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
