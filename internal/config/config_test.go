package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	os.Setenv("TEST_TOKEN", "secret123")
	defer os.Unsetenv("TEST_TOKEN")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
server:
  port: 9090
  internal_token: "${TEST_TOKEN}"
trello:
  lists:
    ready: "abc123"
`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.InternalToken != "secret123" {
		t.Errorf("token = %s, want secret123", cfg.Server.InternalToken)
	}
	if cfg.Trello.Lists["ready"] != "abc123" {
		t.Errorf("list = %s, want abc123", cfg.Trello.Lists["ready"])
	}
	if cfg.ListIDToName("abc123") != "ready" {
		t.Errorf("ListIDToName = %s, want ready", cfg.ListIDToName("abc123"))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`{{{invalid`), 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`server: {}`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Gateway.AgentID != "main" {
		t.Errorf("default agentID = %s, want main", cfg.Gateway.AgentID)
	}
	if cfg.Audit.LogPath != "data/audit.log" {
		t.Errorf("default audit path = %s", cfg.Audit.LogPath)
	}
}

func TestListIDToName_Known(t *testing.T) {
	cfg := &Config{Trello: TrelloConfig{Lists: map[string]string{"ready": "abc"}}}
	if cfg.ListIDToName("abc") != "ready" {
		t.Error("expected ready")
	}
}

func TestListIDToName_Unknown(t *testing.T) {
	cfg := &Config{Trello: TrelloConfig{Lists: map[string]string{"ready": "abc"}}}
	if cfg.ListIDToName("unknown") != "" {
		t.Error("expected empty")
	}
}

func TestEnvSubst_MultipleVars(t *testing.T) {
	os.Setenv("VAR_A", "hello")
	os.Setenv("VAR_B", "world")
	defer os.Unsetenv("VAR_A")
	defer os.Unsetenv("VAR_B")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
server:
  internal_token: "${VAR_A}-${VAR_B}"
`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.InternalToken != "hello-world" {
		t.Errorf("expected hello-world, got %s", cfg.Server.InternalToken)
	}
}

func TestEnvSubst_UnsetVar(t *testing.T) {
	os.Unsetenv("UNSET_VAR_XYZ")
	result := envSubst("${UNSET_VAR_XYZ}")
	if result != "${UNSET_VAR_XYZ}" {
		t.Errorf("unset var should remain as-is, got %s", result)
	}
}

func TestValidate_GatewayRequired(t *testing.T) {
	cfg := &Config{
		Trello: TrelloConfig{Rules: []TrelloRule{{Event: "card_moved"}}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing gateway URL")
	}
	if !strings.Contains(err.Error(), "gateway.url") {
		t.Errorf("expected gateway.url error, got: %v", err)
	}
}

func TestValidate_GmailEmailEmpty(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{URL: "http://localhost"},
		Gmail: GmailConfig{
			Enabled:  true,
			Accounts: []GmailAccountConf{{Email: ""}},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty gmail email")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_GmailEmailNotAllowed(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{URL: "http://localhost"},
		Google:  GoogleConfig{AllowedEmails: []string{"allowed@test.com"}},
		Gmail: GmailConfig{
			Enabled:  true,
			Accounts: []GmailAccountConf{{Email: "other@test.com"}},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for gmail email not in allowed list")
	}
	if !strings.Contains(err.Error(), "not in google.allowed_emails") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_OK(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{InternalToken: "tok"},
		Gateway: GatewayConfig{URL: "http://localhost"},
		Google:  GoogleConfig{AllowedEmails: []string{"test@test.com"}},
		Gmail: GmailConfig{
			Enabled:  true,
			Accounts: []GmailAccountConf{{Email: "test@test.com"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_NoRules_NoGateway(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("no rules + no gateway should be valid: %v", err)
	}
}

func TestDefaultGitHubMessageTemplate(t *testing.T) {
	tmpl := DefaultGitHubMessageTemplate()
	if !strings.Contains(tmpl, "{{.Event}}") {
		t.Error("expected {{.Event}} in default template")
	}
	if !strings.Contains(tmpl, "{{.Repository}}") {
		t.Error("expected {{.Repository}} in default template")
	}
}

func TestGitHubConfig_Fields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
github:
  secret: "mysecret"
  agent_id: "work"
  message_template: "Custom: {{.Event}}"
`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub.AgentID != "work" {
		t.Errorf("expected agent_id work, got %s", cfg.GitHub.AgentID)
	}
	if cfg.GitHub.MessageTemplate != "Custom: {{.Event}}" {
		t.Errorf("expected custom template, got %q", cfg.GitHub.MessageTemplate)
	}
}

func TestGatewayConfig_Model(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
gateway:
  url: "http://localhost"
  model: "my-model"
`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.Model != "my-model" {
		t.Errorf("expected my-model, got %s", cfg.Gateway.Model)
	}
}
