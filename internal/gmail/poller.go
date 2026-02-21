package gmail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/gateway"
)

// GmailState persists the last known historyId.
type GmailState struct {
	HistoryID uint64 `json:"history_id"`
}

// Poller polls Gmail for new messages using historyId.
type Poller struct {
	client       GmailClient
	accountEmail string
	rules        []config.GmailRule
	interval     time.Duration
	gateway      gateway.GatewayClient
	stateDir     string
}

func NewPoller(client GmailClient, cfg *config.GmailConfig, gw gateway.GatewayClient, stateDir string) *Poller {
	return NewPollerForAccount(client, "", cfg.PollInterval, cfg.Rules, gw, stateDir)
}

func NewPollerForAccount(client GmailClient, accountEmail, pollInterval string, rules []config.GmailRule, gw gateway.GatewayClient, stateDir string) *Poller {
	interval := 60 * time.Second
	if pollInterval != "" {
		if d, err := time.ParseDuration(pollInterval); err == nil {
			interval = d
		}
	}
	return &Poller{
		client:       client,
		accountEmail: accountEmail,
		rules:        rules,
		interval:     interval,
		gateway:      gw,
		stateDir:     stateDir,
	}
}

func (p *Poller) stateFile() string {
	if p.accountEmail == "" {
		return filepath.Join(p.stateDir, "gmail-state.json")
	}
	safe := strings.ReplaceAll(p.accountEmail, "/", "_")
	safe = strings.ReplaceAll(safe, "@", "_at_")
	return filepath.Join(p.stateDir, fmt.Sprintf("gmail-state-%s.json", safe))
}

func (p *Poller) loadState() (*GmailState, error) {
	data, err := os.ReadFile(p.stateFile())
	if err != nil {
		return nil, err
	}
	var s GmailState
	return &s, json.Unmarshal(data, &s)
}

func (p *Poller) saveState(s *GmailState) error {
	os.MkdirAll(p.stateDir, 0700)
	data, _ := json.Marshal(s)
	return os.WriteFile(p.stateFile(), data, 0600)
}

// Start begins polling in a goroutine. Cancel ctx to stop.
func (p *Poller) Start(ctx context.Context) {
	go func() {
		log.Printf("Gmail poller starting (account: %q, interval: %s, rules: %d)", p.accountEmail, p.interval, len(p.rules))

		// Initialize historyId if needed
		state, err := p.loadState()
		if err != nil {
			log.Printf("No saved Gmail state, initializing...")
			hid, err := p.client.GetCurrentHistoryID(ctx)
			if err != nil {
				log.Printf("Failed to get initial historyId: %v (will retry)", err)
			} else {
				state = &GmailState{HistoryID: hid}
				p.saveState(state)
				log.Printf("Gmail poller initialized with historyId: %d", hid)
			}
		} else {
			log.Printf("Gmail poller resuming from historyId: %d", state.HistoryID)
		}

		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Gmail poller stopped")
				return
			case <-ticker.C:
				p.poll(ctx)
			}
		}
	}()
}

func (p *Poller) poll(ctx context.Context) {
	state, err := p.loadState()
	if err != nil || state.HistoryID == 0 {
		// Try to initialize
		hid, err := p.client.GetCurrentHistoryID(ctx)
		if err != nil {
			log.Printf("Gmail poll: can't get historyId: %v", err)
			return
		}
		state = &GmailState{HistoryID: hid}
		p.saveState(state)
		return
	}

	msgs, newHID, err := p.client.GetHistory(ctx, state.HistoryID)
	if err != nil {
		// historyId may be too old — reset
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "notFound") {
			log.Printf("Gmail poll: historyId expired, resetting")
			hid, err := p.client.GetCurrentHistoryID(ctx)
			if err == nil {
				p.saveState(&GmailState{HistoryID: hid})
			}
			return
		}
		log.Printf("Gmail poll error: %v", err)
		return
	}

	if newHID > state.HistoryID {
		state.HistoryID = newHID
		p.saveState(state)
	}

	if len(msgs) == 0 {
		return
	}

	log.Printf("Gmail poll: %d new messages", len(msgs))
	for _, msg := range msgs {
		p.evaluateRules(ctx, msg)
	}
}

func (p *Poller) evaluateRules(ctx context.Context, msg HistoryMessage) {
	for _, rule := range p.rules {
		if !p.matchRule(rule.Match, msg) {
			continue
		}
		log.Printf("Gmail rule '%s' matched message %s: %s", rule.Name, msg.ID, msg.Subject)
		if rule.Action.Notify != nil {
			p.executeNotify(ctx, rule.Action.Notify, msg)
		}
	}
}

func (p *Poller) matchRule(match config.GmailMatch, msg HistoryMessage) bool {
	// Match labels
	if len(match.Labels) > 0 {
		msgLabels := make(map[string]bool, len(msg.Labels))
		for _, l := range msg.Labels {
			msgLabels[l] = true
		}
		for _, required := range match.Labels {
			if !msgLabels[required] {
				return false
			}
		}
	}
	// Match from
	if len(match.From) > 0 {
		matched := false
		fromLower := strings.ToLower(msg.From)
		for _, pattern := range match.From {
			pattern = strings.ToLower(pattern)
			if strings.HasPrefix(pattern, "*") {
				if strings.HasSuffix(fromLower, pattern[1:]) {
					matched = true
					break
				}
			} else if strings.Contains(fromLower, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (p *Poller) executeNotify(_ context.Context, notify *config.GmailNotifyAction, msg HistoryMessage) {
	tmplStr := notify.Template
	if tmplStr == "" {
		tmplStr = "📧 {{.From}}: {{.Subject}}"
	}

	tmpl, err := template.New("notify").Parse(tmplStr)
	if err != nil {
		log.Printf("Gmail notify template error: %v", err)
		return
	}

	data := map[string]string{
		"From":    msg.From,
		"Subject": msg.Subject,
		"Snippet": msg.Snippet,
		"ID":      msg.ID,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("Gmail notify template exec error: %v", err)
		return
	}

	message := buf.String()

	// Use gateway to send notification via cron one-shot
	jobMsg := fmt.Sprintf("Send this exact message to Telegram (target=%s, channel=%s). Just send it, no extra text:\n\n%s",
		notify.Target, notify.Channel, message)

	if err := p.gateway.CreateOneShotJobForAgent("gmail-notify", jobMsg, notify.AgentID, 30, 0); err != nil {
		log.Printf("Gmail notify: failed to create gateway job: %v", err)
	}
}
