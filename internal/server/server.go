package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/katalabut/openclaw-relay/internal/audit"
	"github.com/katalabut/openclaw-relay/internal/auth"
	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/gateway"
	"github.com/katalabut/openclaw-relay/internal/gmail"
	"github.com/katalabut/openclaw-relay/internal/ratelimit"
	"github.com/katalabut/openclaw-relay/internal/tokens"
	"github.com/katalabut/openclaw-relay/internal/webhook"
)

func Run(cfg *config.Config) error {
	gw := gateway.NewClient(cfg.Gateway.URL, cfg.Gateway.Token, cfg.Gateway.AgentID)
	limiter := ratelimit.New(5 * time.Minute)

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Webhooks
	mux.Handle("/webhook/trello", &webhook.TrelloHandler{Config: cfg, Gateway: gw, Limiter: limiter})
	mux.Handle("/webhook/github", &webhook.GitHubHandler{Config: cfg, Gateway: gw, Limiter: limiter})

	// Token store + Google OAuth
	var googleAuth *auth.GoogleAuth
	encKey := os.Getenv("RELAY_ENCRYPTION_KEY")
	if encKey != "" && cfg.Google.ClientID != "" {
		store, err := tokens.NewStore("data/tokens.json.enc", encKey)
		if err != nil {
			log.Printf("Warning: token store init failed: %v", err)
		} else {
			googleAuth = auth.NewGoogleAuth(&cfg.Google, store)
			googleAuth.RegisterRoutes(mux)

			// Auth status API
			mux.HandleFunc("/api/auth/status", googleAuth.HandleAuthStatus)

			// Gmail
			if cfg.Gmail.Enabled {
				accounts := cfg.Gmail.ResolvedAccounts(cfg.Google.AllowedEmails)
				if len(accounts) == 0 {
					// Fallback route support for direct API usage.
					gmailClient := gmail.NewClient(store, googleAuth.OAuthConfig())
					gmailHandler := gmail.NewHandler(gmailClient)
					gmailHandler.RegisterRoutes(mux)
					log.Println("Gmail enabled but no account rules configured")
				} else {
					// Register API routes using first account by default.
					defaultClient := gmail.NewClientForAccount(store, googleAuth.OAuthConfig(), accounts[0].Email)
					gmailHandler := gmail.NewHandler(defaultClient)
					gmailHandler.RegisterRoutes(mux)

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					for _, acc := range accounts {
						client := gmail.NewClientForAccount(store, googleAuth.OAuthConfig(), acc.Email)
						poller := gmail.NewPollerForAccount(client, acc.Email, acc.PollInterval, acc.Rules, gw, "data")
						poller.Start(ctx)
					}
					log.Printf("Gmail integration enabled for %d account(s)", len(accounts))
				}
			}
		}
	} else {
		// Default root page
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>openclaw-relay</h2><p>Google OAuth not configured.</p></body></html>`)
		})
	}

	// API status
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"openclaw-relay"}`))
	})

	// Wrap with auth middleware
	var handler http.Handler = mux
	if cfg.Server.InternalToken != "" {
		handler = auth.Middleware(cfg.Server.InternalToken, handler)
	}

	// Wrap with audit middleware
	auditLogger, err := audit.NewLogger(cfg.Audit.LogPath)
	if err != nil {
		log.Printf("Warning: audit log disabled: %v", err)
	} else {
		handler = audit.Middleware(auditLogger, handler)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("openclaw-relay starting on %s", addr)
	log.Printf("Agent: %s, Gateway: %s", cfg.Gateway.AgentID, cfg.Gateway.URL)
	return http.ListenAndServe(addr, handler)
}
