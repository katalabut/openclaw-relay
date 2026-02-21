package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

func TestNewPoller(t *testing.T) {
	cfg := &config.GmailConfig{
		PollInterval: "30s",
		Rules:        []config.GmailRule{{Name: "test"}},
	}
	mc := &mockGmailClient{} // from handler_test.go — same package
	gw := &mockGW{}
	p := NewPoller(mc, cfg, gw, t.TempDir())
	if p.interval.Seconds() != 30 {
		t.Errorf("expected 30s interval, got %v", p.interval)
	}
	if len(p.rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(p.rules))
	}
}

func TestNewPoller_DefaultInterval(t *testing.T) {
	cfg := &config.GmailConfig{}
	mc := &mockGmailClient{}
	gw := &mockGW{}
	p := NewPoller(mc, cfg, gw, t.TempDir())
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
	p := &Poller{stateDir: dir}

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
	data, _ := os.ReadFile(filepath.Join(dir, "gmail-state.json"))
	var s GmailState
	json.Unmarshal(data, &s)
	if s.HistoryID != 12345 {
		t.Errorf("file content mismatch")
	}
}
