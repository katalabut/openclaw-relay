package config

import (
	"os"
	"path/filepath"
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
