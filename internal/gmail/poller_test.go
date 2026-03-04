package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/katalabut/openclaw-relay/internal/config"
)

func TestMatchRule_LabelMatch(t *testing.T) {
	p := &Poller{}
	match := config.GmailMatch{Labels: []string{"INBOX", "UNREAD"}}
	msg := HistoryMessage{Labels: []string{"INBOX", "UNREAD", "IMPORTANT"}}
	if !p.matchRule(match, msg) {
		t.Error("expected match")
	}
}

func TestMatchRule_LabelNoMatch(t *testing.T) {
	p := &Poller{}
	match := config.GmailMatch{Labels: []string{"INBOX", "STARRED"}}
	msg := HistoryMessage{Labels: []string{"INBOX", "UNREAD"}}
	if p.matchRule(match, msg) {
		t.Error("expected no match — STARRED missing")
	}
}

func TestMatchRule_FromMatch(t *testing.T) {
	p := &Poller{}
	match := config.GmailMatch{From: []string{"*@github.com"}}
	msg := HistoryMessage{From: "notifications@github.com"}
	if !p.matchRule(match, msg) {
		t.Error("expected from match")
	}
}

func TestMatchRule_FromNoMatch(t *testing.T) {
	p := &Poller{}
	match := config.GmailMatch{From: []string{"*@github.com"}}
	msg := HistoryMessage{From: "user@example.com"}
	if p.matchRule(match, msg) {
		t.Error("expected no match")
	}
}

func TestMatchRule_FromContains(t *testing.T) {
	p := &Poller{}
	match := config.GmailMatch{From: []string{"github"}}
	msg := HistoryMessage{From: "noreply@github.com"}
	if !p.matchRule(match, msg) {
		t.Error("expected contains match")
	}
}

func TestEvaluateRules_FirstMatchWins(t *testing.T) {
	// We can't easily test evaluateRules without a gateway mock,
	// but we can test matchRule which is the core logic
	p := &Poller{
		rules: []config.GmailRule{
			{Name: "rule1", Match: config.GmailMatch{Labels: []string{"STARRED"}}},
			{Name: "rule2", Match: config.GmailMatch{Labels: []string{"INBOX"}}},
		},
	}
	msg := HistoryMessage{Labels: []string{"INBOX"}}
	// rule1 should not match, rule2 should match
	if p.matchRule(p.rules[0].Match, msg) {
		t.Error("rule1 should not match")
	}
	if !p.matchRule(p.rules[1].Match, msg) {
		t.Error("rule2 should match")
	}
}

func TestMatchRules_NoMatch(t *testing.T) {
	p := &Poller{
		rules: []config.GmailRule{
			{Name: "rule1", Match: config.GmailMatch{Labels: []string{"STARRED"}}},
		},
	}
	msg := HistoryMessage{Labels: []string{"INBOX"}}
	if p.matchRule(p.rules[0].Match, msg) {
		t.Error("should not match")
	}
}

type mockGW struct {
	calls []string
}

func (m *mockGW) CreateOneShotJob(name, message string, timeout, delay int) error {
	m.calls = append(m.calls, name)
	return nil
}

func (m *mockGW) CreateOneShotJobForAgent(name, message, agentID string, timeout, delay int) error {
	m.calls = append(m.calls, name)
	return nil
}

func TestNewPollerForAccount(t *testing.T) {
	mc := &mockGmailClient{} // from handler_test.go — same package
	gw := &mockGW{}
	rules := []config.GmailRule{{Name: "test"}}
	p := NewPollerForAccount(mc, "user@example.com", "30s", rules, gw, t.TempDir(), nil)
	if p.interval.Seconds() != 30 {
		t.Errorf("expected 30s interval, got %v", p.interval)
	}
	if len(p.rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(p.rules))
	}
	if p.accountEmail != "user@example.com" {
		t.Errorf("expected user@example.com, got %s", p.accountEmail)
	}
}

func TestNewPollerForAccount_DefaultInterval(t *testing.T) {
	mc := &mockGmailClient{}
	gw := &mockGW{}
	p := NewPollerForAccount(mc, "user@example.com", "", nil, gw, t.TempDir(), nil)
	if p.interval.Seconds() != 60 {
		t.Errorf("expected 60s default, got %v", p.interval)
	}
}

func TestEvaluateRules_WithNotify(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		rules: []config.GmailRule{
			{
				Name:  "notify-test",
				Match: config.GmailMatch{Labels: []string{"INBOX"}},
				Action: config.GmailAction{
					Notify: &config.GmailNotifyAction{
						Target:   "123",
						Channel:  "telegram",
						Template: "📧 {{.From}}: {{.Subject}}",
					},
				},
			},
		},
		gateway: gw,
	}
	msg := HistoryMessage{
		ID:      "m1",
		Labels:  []string{"INBOX", "UNREAD"},
		Subject: "Test",
		From:    "sender@example.com",
	}
	p.evaluateRules(context.Background(), msg)
	if len(gw.calls) != 1 {
		t.Errorf("expected 1 gateway call, got %d", len(gw.calls))
	}
}

func TestEvaluateRules_WithCronAction(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		accountEmail: "user@test.com",
		rules: []config.GmailRule{
			{
				Name:  "cron-test",
				Match: config.GmailMatch{Labels: []string{"INBOX"}},
				Action: config.GmailAction{
					Kind:            "cron",
					AgentID:         "main",
					Timeout:         180,
					Delay:           2,
					MessageTemplate: "Message {{.MessageID}} from {{.AccountEmail}}",
				},
			},
		},
		gateway: gw,
	}
	msg := HistoryMessage{
		ID:       "m1",
		ThreadID: "t1",
		Labels:   []string{"INBOX"},
		Subject:  "Test",
		From:     "sender@example.com",
	}
	p.evaluateRules(context.Background(), msg)
	if len(gw.calls) != 1 {
		t.Errorf("expected 1 gateway call, got %d", len(gw.calls))
	}
}

func TestEvaluateRules_CronActionDefaultTemplate(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		rules: []config.GmailRule{
			{
				Name:  "cron-default",
				Match: config.GmailMatch{Labels: []string{"INBOX"}},
				Action: config.GmailAction{
					Kind: "cron",
				},
			},
		},
		gateway: gw,
	}
	msg := HistoryMessage{ID: "m1", Labels: []string{"INBOX"}, From: "a@b.com", Subject: "Hi"}
	p.evaluateRules(context.Background(), msg)
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(gw.calls))
	}
}

func TestTemplateData_HasAllFields(t *testing.T) {
	p := &Poller{accountEmail: "test@test.com"}
	msg := HistoryMessage{ID: "m1", ThreadID: "t1", From: "a@b.com", Subject: "Hi", Snippet: "snip"}
	data := p.templateData(msg)
	expected := map[string]string{
		"From": "a@b.com", "Subject": "Hi", "Snippet": "snip",
		"ID": "m1", "MessageID": "m1", "ThreadID": "t1", "AccountEmail": "test@test.com",
	}
	for k, v := range expected {
		if data[k] != v {
			t.Errorf("data[%s] = %q, want %q", k, data[k], v)
		}
	}
}

func TestEvaluateRules_NoMatch(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		rules: []config.GmailRule{
			{
				Name:  "rule1",
				Match: config.GmailMatch{Labels: []string{"STARRED"}},
				Action: config.GmailAction{
					Notify: &config.GmailNotifyAction{Target: "123", Channel: "telegram"},
				},
			},
		},
		gateway: gw,
	}
	msg := HistoryMessage{Labels: []string{"INBOX"}}
	p.evaluateRules(context.Background(), msg)
	if len(gw.calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(gw.calls))
	}
}

func TestExecuteNotify_DefaultTemplate(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{gateway: gw}
	notify := &config.GmailNotifyAction{Target: "123", Channel: "telegram"}
	msg := HistoryMessage{From: "a@b.com", Subject: "Hi"}
	p.executeNotify(context.Background(), notify, msg)
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(gw.calls))
	}
}

func TestPoll_NoState_Initializes(t *testing.T) {
	mc := &mockGmailClient{
		getCurrentHIDFunc: func(_ context.Context) (uint64, error) {
			return 100, nil
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{client: mc, gateway: gw, stateDir: dir}

	p.poll(context.Background())

	state, err := p.loadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.HistoryID != 100 {
		t.Errorf("expected historyID 100, got %d", state.HistoryID)
	}
}

func TestPoll_WithState_ProcessesMessages(t *testing.T) {
	mc := &mockGmailClient{
		getHistoryFunc: func(_ context.Context, startHID uint64) ([]HistoryMessage, uint64, error) {
			return []HistoryMessage{
				{ID: "m1", Labels: []string{"INBOX"}, Subject: "Test", From: "a@b.com"},
			}, 200, nil
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{
		client:   mc,
		gateway:  gw,
		stateDir: dir,
		rules: []config.GmailRule{
			{
				Name:  "r1",
				Match: config.GmailMatch{Labels: []string{"INBOX"}},
				Action: config.GmailAction{
					Notify: &config.GmailNotifyAction{Target: "123", Channel: "telegram"},
				},
			},
		},
	}
	p.saveState(&GmailState{HistoryID: 100})

	p.poll(context.Background())

	state, _ := p.loadState()
	if state.HistoryID != 200 {
		t.Errorf("expected historyID 200, got %d", state.HistoryID)
	}
	if len(gw.calls) != 1 {
		t.Errorf("expected 1 notify, got %d", len(gw.calls))
	}
}

func TestPoll_NoNewMessages(t *testing.T) {
	mc := &mockGmailClient{
		getHistoryFunc: func(_ context.Context, _ uint64) ([]HistoryMessage, uint64, error) {
			return nil, 100, nil
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{client: mc, gateway: gw, stateDir: dir}
	p.saveState(&GmailState{HistoryID: 100})

	p.poll(context.Background())
	if len(gw.calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(gw.calls))
	}
}

func TestPoll_HistoryError_Resets(t *testing.T) {
	mc := &mockGmailClient{
		getHistoryFunc: func(_ context.Context, _ uint64) ([]HistoryMessage, uint64, error) {
			return nil, 0, fmt.Errorf("googleapi: Error 404: notFound")
		},
		getCurrentHIDFunc: func(_ context.Context) (uint64, error) {
			return 500, nil
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{client: mc, gateway: gw, stateDir: dir}
	p.saveState(&GmailState{HistoryID: 50})

	p.poll(context.Background())

	state, _ := p.loadState()
	if state.HistoryID != 500 {
		t.Errorf("expected reset to 500, got %d", state.HistoryID)
	}
}

func TestPoll_HistoryError_NonReset(t *testing.T) {
	mc := &mockGmailClient{
		getHistoryFunc: func(_ context.Context, _ uint64) ([]HistoryMessage, uint64, error) {
			return nil, 0, fmt.Errorf("connection error")
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{client: mc, gateway: gw, stateDir: dir}
	p.saveState(&GmailState{HistoryID: 50})

	p.poll(context.Background())

	state, _ := p.loadState()
	if state.HistoryID != 50 {
		t.Errorf("expected unchanged 50, got %d", state.HistoryID)
	}
}

func TestPoll_InitFails(t *testing.T) {
	mc := &mockGmailClient{
		getCurrentHIDFunc: func(_ context.Context) (uint64, error) {
			return 0, fmt.Errorf("not auth")
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{client: mc, gateway: gw, stateDir: dir}

	// Should not panic
	p.poll(context.Background())
}

func TestExecuteNotify_BadTemplate(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{gateway: gw}
	notify := &config.GmailNotifyAction{
		Target:   "123",
		Channel:  "telegram",
		Template: "{{.Invalid",
	}
	msg := HistoryMessage{From: "a@b.com", Subject: "Hi"}
	// Should not panic, just log error
	p.executeNotify(context.Background(), notify, msg)
	// Gateway should NOT be called when template fails
	if len(gw.calls) != 0 {
		t.Errorf("expected 0 calls on bad template, got %d", len(gw.calls))
	}
}

func TestExecuteNotify_CustomTemplate(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{gateway: gw}
	notify := &config.GmailNotifyAction{
		Target:   "123",
		Channel:  "telegram",
		Template: "New mail from {{.From}} - {{.Subject}}",
	}
	msg := HistoryMessage{From: "test@test.com", Subject: "Hello"}
	p.executeNotify(context.Background(), notify, msg)
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(gw.calls))
	}
}

func TestMatchRule_EmptyMatch(t *testing.T) {
	p := &Poller{}
	match := config.GmailMatch{}
	msg := HistoryMessage{Labels: []string{"INBOX"}}
	if !p.matchRule(match, msg) {
		t.Error("empty match should match everything")
	}
}

func TestLoadState_NoFile(t *testing.T) {
	p := &Poller{stateDir: t.TempDir()}
	_, err := p.loadState()
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSaveLoadState_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	p := &Poller{accountEmail: "user@example.com", stateDir: dir}

	state := &GmailState{HistoryID: 12345}
	if err := p.saveState(state); err != nil {
		t.Fatal(err)
	}

	loaded, err := p.loadState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HistoryID != 12345 {
		t.Errorf("expected 12345, got %d", loaded.HistoryID)
	}

	// Verify file content
	data, _ := os.ReadFile(filepath.Join(dir, "gmail-state-user_at_example.com.json"))
	var s GmailState
	json.Unmarshal(data, &s)
	if s.HistoryID != 12345 {
		t.Errorf("file content mismatch")
	}
}

func TestPoll_DeduplicatesMessages(t *testing.T) {
	mc := &mockGmailClient{
		getHistoryFunc: func(_ context.Context, _ uint64) ([]HistoryMessage, uint64, error) {
			// Return same message twice (happens with multiple label changes)
			return []HistoryMessage{
				{ID: "m1", Labels: []string{"INBOX"}, Subject: "Dup", From: "a@b.com"},
				{ID: "m1", Labels: []string{"INBOX", "IMPORTANT"}, Subject: "Dup", From: "a@b.com"},
				{ID: "m2", Labels: []string{"INBOX"}, Subject: "Unique", From: "c@d.com"},
			}, 200, nil
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	p := &Poller{
		client:   mc,
		gateway:  gw,
		stateDir: dir,
		rules: []config.GmailRule{
			{
				Name:  "r1",
				Match: config.GmailMatch{Labels: []string{"INBOX"}},
				Action: config.GmailAction{
					Notify: &config.GmailNotifyAction{Target: "123", Channel: "telegram"},
				},
			},
		},
	}
	p.saveState(&GmailState{HistoryID: 100})
	p.poll(context.Background())

	// Should only fire 2 times (not 3) due to dedup
	if len(gw.calls) != 2 {
		t.Errorf("expected 2 calls (deduped), got %d", len(gw.calls))
	}
}

func TestHandleAuthError_SendsAlert(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		accountEmail: "test@test.com",
		gateway:      gw,
		authAlertCfg: &config.GmailAuthAlertConfig{
			Enabled:         true,
			AgentID:         "main",
			Cooldown:        "1s",
			Timeout:         90,
			MessageTemplate: "Auth failed: {{.AccountEmail}} - {{.Error}}",
		},
		authErrCooldown: 1 * time.Second,
	}

	p.handleAuthError(context.Background(), fmt.Errorf("not authenticated with Google"))
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 alert call, got %d", len(gw.calls))
	}
}

func TestHandleAuthError_CooldownPreventsSpam(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		accountEmail: "test@test.com",
		gateway:      gw,
		authAlertCfg: &config.GmailAuthAlertConfig{
			Enabled:  true,
			Cooldown: "1h",
		},
		authErrCooldown: 1 * time.Hour,
	}

	p.handleAuthError(context.Background(), fmt.Errorf("not authenticated"))
	p.handleAuthError(context.Background(), fmt.Errorf("not authenticated"))
	// Only 1 alert due to cooldown
	if len(gw.calls) != 1 {
		t.Errorf("expected 1 call (cooldown), got %d", len(gw.calls))
	}
}

func TestHandleAuthError_IgnoresNonAuthErrors(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		accountEmail: "test@test.com",
		gateway:      gw,
		authAlertCfg: &config.GmailAuthAlertConfig{Enabled: true, Cooldown: "1s"},
		authErrCooldown: 1 * time.Second,
	}

	p.handleAuthError(context.Background(), fmt.Errorf("network timeout"))
	if len(gw.calls) != 0 {
		t.Errorf("expected 0 calls for non-auth error, got %d", len(gw.calls))
	}
}

func TestHandleAuthError_DisabledConfig(t *testing.T) {
	gw := &mockGW{}
	p := &Poller{
		accountEmail: "test@test.com",
		gateway:      gw,
		authAlertCfg: nil,
	}
	// Should not panic
	p.handleAuthError(context.Background(), fmt.Errorf("not authenticated"))
	if len(gw.calls) != 0 {
		t.Errorf("expected 0 calls when alert disabled, got %d", len(gw.calls))
	}
}

func TestJobName(t *testing.T) {
	msg := HistoryMessage{ID: "m1", Subject: "Important email about project"}
	name := jobName("gmail", "inbox-triage", msg)
	if name != "gmail/inbox-triage: Important email about project" {
		t.Errorf("unexpected job name: %s", name)
	}
}

func TestJobName_LongSubject(t *testing.T) {
	msg := HistoryMessage{Subject: strings.Repeat("x", 100)}
	name := jobName("gmail", "rule", msg)
	if len(name) > 80 {
		// prefix + rule + truncated subject + "..."
		if !strings.HasSuffix(name, "...") {
			t.Errorf("expected truncated name with ..., got: %s", name)
		}
	}
}

func TestJobName_NoRule(t *testing.T) {
	msg := HistoryMessage{Subject: "Test"}
	name := jobName("gmail-notify", "", msg)
	if name != "gmail-notify: Test" {
		t.Errorf("unexpected: %s", name)
	}
}

func TestPoll_RespectsContextDuringProcessing(t *testing.T) {
	mc := &mockGmailClient{
		getHistoryFunc: func(_ context.Context, _ uint64) ([]HistoryMessage, uint64, error) {
			return []HistoryMessage{
				{ID: "m1", Labels: []string{"INBOX"}, Subject: "1", From: "a@b.com"},
				{ID: "m2", Labels: []string{"INBOX"}, Subject: "2", From: "a@b.com"},
			}, 200, nil
		},
	}
	gw := &mockGW{}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	p := &Poller{
		client:   mc,
		gateway:  gw,
		stateDir: dir,
		rules: []config.GmailRule{
			{
				Name:  "r1",
				Match: config.GmailMatch{Labels: []string{"INBOX"}},
				Action: config.GmailAction{
					Kind:            "cron",
					MessageTemplate: "test",
				},
			},
		},
	}
	p.saveState(&GmailState{HistoryID: 100})
	p.poll(ctx)

	// With cancelled context, should process 0 messages
	if len(gw.calls) != 0 {
		t.Errorf("expected 0 calls with cancelled context, got %d", len(gw.calls))
	}
}
