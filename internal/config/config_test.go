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
