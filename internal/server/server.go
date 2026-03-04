package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	gw := gateway.NewClient(cfg.Gateway.URL, cfg.Gateway.Token, cfg.Gateway.AgentID, cfg.Gateway.Model)
	limiter := ratelimit.New(ctx, 5*time.Minute)

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
	var auditLogger *audit.Logger
	encKey := os.Getenv("RELAY_ENCRYPTION_KEY")
	if encKey != "" && cfg.Google.ClientID != "" {
		store, err := tokens.NewStore("data/tokens.json.enc", encKey)
		if err != nil {
			log.Printf("Warning: token store init failed: %v", err)
		} else {
			googleAuth = auth.NewGoogleAuth(ctx, &cfg.Google, store, encKey, cfg)
			googleAuth.RegisterRoutes(mux)

			// Auth status API
			mux.HandleFunc("/api/auth/status", googleAuth.HandleAuthStatus)

			// Gmail
			if cfg.Gmail.Enabled {
				accounts := cfg.Gmail.ResolvedAccounts()
				if len(accounts) > 0 {
					// Build client map for multi-account API
					clients := make(map[string]gmail.GmailClient, len(accounts))
					for _, acc := range accounts {
						clients[acc.Email] = gmail.NewClientForAccount(store, googleAuth.OAuthConfig(), acc.Email)
					}
					gmailHandler := gmail.NewMultiHandler(clients)
					gmailHandler.RegisterRoutes(mux)

					for _, acc := range accounts {
						client := clients[acc.Email]
						poller := gmail.NewPollerForAccount(client, acc.Email, acc.PollInterval, acc.Rules, gw, "data", cfg.Gmail.AuthAlert)
						poller.Start(ctx)
					}
					log.Printf("Gmail integration enabled for %d account(s)", len(accounts))
				} else {
					log.Println("Gmail enabled but no accounts configured")
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
	var err error
	auditLogger, err = audit.NewLogger(cfg.Audit.LogPath)
	if err != nil {
		log.Printf("Warning: audit log disabled: %v", err)
	} else {
		handler = audit.Middleware(auditLogger, handler)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: handler,
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Printf("openclaw-relay starting on %s", srv.Addr)
		log.Printf("Agent: %s, Gateway: %s", cfg.Gateway.AgentID, cfg.Gateway.URL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown: stop HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Close audit logger
	if auditLogger != nil {
		auditLogger.Close()
	}

	log.Println("Server stopped")
	return nil
}
