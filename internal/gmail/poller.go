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

	// auth failure tracking
	lastAuthErr     time.Time
	authAlertCfg    *config.GmailAuthAlertConfig
	authErrCooldown time.Duration
}

func NewPollerForAccount(client GmailClient, accountEmail, pollInterval string, rules []config.GmailRule, gw gateway.GatewayClient, stateDir string, authAlert *config.GmailAuthAlertConfig) *Poller {
	interval := 60 * time.Second
	if pollInterval != "" {
		if d, err := time.ParseDuration(pollInterval); err == nil {
			interval = d
		}
	}
	cooldown := 30 * time.Minute
	if authAlert != nil && authAlert.Cooldown != "" {
		if d, err := time.ParseDuration(authAlert.Cooldown); err == nil {
			cooldown = d
		}
	}
	return &Poller{
		client:          client,
		accountEmail:    accountEmail,
		rules:           rules,
		interval:        interval,
		gateway:         gw,
		stateDir:        stateDir,
		authAlertCfg:    authAlert,
		authErrCooldown: cooldown,
	}
}

func (p *Poller) stateFile() string {
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
				p.handleAuthError(ctx, err)
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
				log.Printf("Gmail poller stopped (account: %s)", p.accountEmail)
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
			p.handleAuthError(ctx, err)
			return
		}
		if state != nil && state.HistoryID > 0 {
			log.Printf("Gmail poll: WARNING reinitializing from historyId %d → %d, messages in between may be lost", state.HistoryID, hid)
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
				log.Printf("Gmail poll: WARNING historyId reset from %d → %d, messages in between are lost", state.HistoryID, hid)
				p.saveState(&GmailState{HistoryID: hid})
			}
			return
		}
		log.Printf("Gmail poll error: %v", err)
		p.handleAuthError(ctx, err)
		return
	}

	if newHID > state.HistoryID {
		state.HistoryID = newHID
		p.saveState(state)
	}

	if len(msgs) == 0 {
		return
	}

	// Dedup messages by ID (History API can return duplicates)
	seen := make(map[string]bool, len(msgs))
	unique := make([]HistoryMessage, 0, len(msgs))
	for _, msg := range msgs {
		if seen[msg.ID] {
			continue
		}
		seen[msg.ID] = true
		unique = append(unique, msg)
	}

	log.Printf("Gmail poll: %d new messages (%d after dedup)", len(msgs), len(unique))

	for _, msg := range unique {
		// Respect context on shutdown
		select {
		case <-ctx.Done():
			log.Printf("Gmail poll: shutdown during message processing, %d messages remaining", len(unique)-len(seen))
			return
		default:
		}
		p.evaluateRules(ctx, msg)
	}
}

func (p *Poller) evaluateRules(ctx context.Context, msg HistoryMessage) {
	for _, rule := range p.rules {
		if !p.matchRule(rule.Match, msg) {
			continue
		}
		log.Printf("Gmail rule '%s' matched message %s: %s", rule.Name, msg.ID, msg.Subject)
		if rule.Action.IsCron() {
			p.executeCronAction(ctx, rule, msg)
		} else if rule.Action.Notify != nil {
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

func (p *Poller) templateData(msg HistoryMessage) map[string]string {
	return map[string]string{
		"From":         msg.From,
		"Subject":      msg.Subject,
		"Snippet":      msg.Snippet,
		"ID":           msg.ID,
		"MessageID":    msg.ID,
		"ThreadID":     msg.ThreadID,
		"AccountEmail": p.accountEmail,
	}
}

func (p *Poller) renderTemplate(name, tmplStr string, data map[string]string) (string, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template exec: %w", err)
	}
	return buf.String(), nil
}

// jobName creates a descriptive job name from rule and message.
func jobName(prefix, ruleName string, msg HistoryMessage) string {
	subject := msg.Subject
	if len(subject) > 50 {
		subject = subject[:50] + "..."
	}
	if ruleName != "" {
		return fmt.Sprintf("%s/%s: %s", prefix, ruleName, subject)
	}
	return fmt.Sprintf("%s: %s", prefix, subject)
}

// executeCronAction sends a cron-style action directly to the gateway.
func (p *Poller) executeCronAction(ctx context.Context, rule config.GmailRule, msg HistoryMessage) {
	// Check context before gateway call
	select {
	case <-ctx.Done():
		return
	default:
	}

	tmplStr := rule.Action.ResolvedTemplate()
	if tmplStr == "" {
		tmplStr = "📧 {{.From}}: {{.Subject}}"
	}

	message, err := p.renderTemplate("cron", tmplStr, p.templateData(msg))
	if err != nil {
		log.Printf("Gmail cron action template error: %v", err)
		return
	}

	name := jobName("gmail", rule.Name, msg)
	if err := p.gateway.CreateOneShotJobForAgent(
		name,
		message,
		rule.Action.ResolvedAgentID(),
		rule.Action.ResolvedTimeout(),
		rule.Action.ResolvedDelay(),
	); err != nil {
		log.Printf("Gmail cron action: failed to create gateway job: %v", err)
	}
}

func (p *Poller) executeNotify(ctx context.Context, notify *config.GmailNotifyAction, msg HistoryMessage) {
	// Check context before gateway call
	select {
	case <-ctx.Done():
		return
	default:
	}

	tmplStr := notify.Template
	if tmplStr == "" {
		tmplStr = "📧 {{.From}}: {{.Subject}}"
	}

	message, err := p.renderTemplate("notify", tmplStr, p.templateData(msg))
	if err != nil {
		log.Printf("Gmail notify template error: %v", err)
		return
	}

	// Use gateway to send notification via cron one-shot
	jobMsg := fmt.Sprintf("Send this exact message to Telegram (target=%s, channel=%s). Just send it, no extra text:\n\n%s",
		notify.Target, notify.Channel, message)

	name := jobName("gmail-notify", "", msg)
	if err := p.gateway.CreateOneShotJobForAgent(name, jobMsg, notify.AgentID, 30, 0); err != nil {
		log.Printf("Gmail notify: failed to create gateway job: %v", err)
	}
}

// handleAuthError sends an alert if the error looks like an auth failure and cooldown has passed.
func (p *Poller) handleAuthError(ctx context.Context, err error) {
	if p.authAlertCfg == nil || !p.authAlertCfg.Enabled {
		return
	}

	errStr := err.Error()
	isAuth := strings.Contains(errStr, "not authenticated") ||
		strings.Contains(errStr, "token refresh") ||
		strings.Contains(errStr, "oauth2") ||
		strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "invalid_grant")
	if !isAuth {
		return
	}

	// Cooldown: don't spam alerts
	if !p.lastAuthErr.IsZero() && time.Since(p.lastAuthErr) < p.authErrCooldown {
		return
	}
	p.lastAuthErr = time.Now()

	select {
	case <-ctx.Done():
		return
	default:
	}

	tmplStr := p.authAlertCfg.MessageTemplate
	if tmplStr == "" {
		tmplStr = "[Relay Alert] Gmail auth failed for {{.AccountEmail}}: {{.Error}}"
	}

	data := map[string]string{
		"AccountEmail": p.accountEmail,
		"Error":        errStr,
		"NowRFC3339":   time.Now().UTC().Format(time.RFC3339),
	}

	message, tmplErr := p.renderTemplate("auth-alert", tmplStr, data)
	if tmplErr != nil {
		log.Printf("Gmail auth alert template error: %v", tmplErr)
		message = fmt.Sprintf("[Relay Alert] Gmail auth failed for %s: %s", p.accountEmail, errStr)
	}

	timeout := p.authAlertCfg.Timeout
	if timeout == 0 {
		timeout = 90
	}

	log.Printf("Gmail auth alert: sending for %s", p.accountEmail)
	if alertErr := p.gateway.CreateOneShotJobForAgent(
		fmt.Sprintf("gmail-auth-alert/%s", p.accountEmail),
		message,
		p.authAlertCfg.AgentID,
		timeout,
		p.authAlertCfg.Delay,
	); alertErr != nil {
		log.Printf("Gmail auth alert: failed to send: %v", alertErr)
	}
}
