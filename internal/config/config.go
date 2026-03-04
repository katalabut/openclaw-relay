package config

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Gateway GatewayConfig `yaml:"gateway"`
	Trello  TrelloConfig  `yaml:"trello"`
	GitHub  GitHubConfig  `yaml:"github"`
	Google  GoogleConfig  `yaml:"google"`
	Gmail   GmailConfig   `yaml:"gmail"`
	Audit   AuditConfig   `yaml:"audit"`
}

type GoogleConfig struct {
	ClientID      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret"`
	RedirectURL   string   `yaml:"redirect_url"`
	AllowedEmails []string `yaml:"allowed_emails"`
}

type GmailConfig struct {
	Enabled      bool                  `yaml:"enabled"`
	PollInterval string                `yaml:"poll_interval"`
	Accounts     []GmailAccountConf    `yaml:"accounts"`
	AuthAlert    *GmailAuthAlertConfig `yaml:"auth_alert"`
}

type GmailAuthAlertConfig struct {
	Enabled         bool   `yaml:"enabled"`
	AgentID         string `yaml:"agent_id"`
	Cooldown        string `yaml:"cooldown"`
	Timeout         int    `yaml:"timeout"`
	Delay           int    `yaml:"delay"`
	MessageTemplate string `yaml:"message_template"`
}

type GmailAccountConf struct {
	Email        string      `yaml:"email"`
	PollInterval string      `yaml:"poll_interval"`
	Rules        []GmailRule `yaml:"rules"`
}

type GmailRule struct {
	Name   string      `yaml:"name"`
	Match  GmailMatch  `yaml:"match"`
	Action GmailAction `yaml:"action"`
}

type GmailMatch struct {
	From   []string `yaml:"from"`
	Labels []string `yaml:"labels"`
	Query  string   `yaml:"query"`
}

type GmailAction struct {
	// Cron-style action (flat format, like Trello rules)
	Kind            string `yaml:"kind"`
	AgentID         string `yaml:"agent_id"`
	Timeout         int    `yaml:"timeout"`
	Delay           int    `yaml:"delay"`
	MessageTemplate string `yaml:"message_template"`

	// Legacy notify sub-action (kept for backward compat)
	Notify *GmailNotifyAction `yaml:"notify"`
}

// ResolvedTemplate returns the message template from either flat or notify format.
func (a GmailAction) ResolvedTemplate() string {
	if a.MessageTemplate != "" {
		return a.MessageTemplate
	}
	if a.Notify != nil && a.Notify.Template != "" {
		return a.Notify.Template
	}
	return ""
}

// ResolvedAgentID returns agent_id from either flat or notify format.
func (a GmailAction) ResolvedAgentID() string {
	if a.AgentID != "" {
		return a.AgentID
	}
	if a.Notify != nil {
		return a.Notify.AgentID
	}
	return ""
}

// ResolvedTimeout returns timeout with default 30.
func (a GmailAction) ResolvedTimeout() int {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return 30
}

// ResolvedDelay returns delay with default 0.
func (a GmailAction) ResolvedDelay() int {
	return a.Delay
}

// IsCron returns true if this is a direct cron-style action (not legacy notify).
func (a GmailAction) IsCron() bool {
	return a.Kind == "cron" || a.MessageTemplate != ""
}

type GmailNotifyAction struct {
	Target   string `yaml:"target"`
	Channel  string `yaml:"channel"`
	Template string `yaml:"template"`
	AgentID  string `yaml:"agent_id"` // optional: which agent sends the notification (default: global)
}

type ServerConfig struct {
	Port          int    `yaml:"port"`
	InternalToken string `yaml:"internal_token"`
}

type GatewayConfig struct {
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	AgentID string `yaml:"agent_id"`
	Model   string `yaml:"model"`
}

type TrelloConfig struct {
	Secret        string            `yaml:"secret"`
	Lists         map[string]string `yaml:"lists"`
	IgnoreMembers []string          `yaml:"ignore_members"` // member IDs or usernames to ignore (e.g. bot accounts)
	Rules         []TrelloRule      `yaml:"rules"`
}

type TrelloRule struct {
	Event     string     `yaml:"event"`
	Condition string     `yaml:"condition"`
	Action    RuleAction `yaml:"action"`
}

type RuleAction struct {
	Kind            string `yaml:"kind"`
	Timeout         int    `yaml:"timeout"`
	Delay           int    `yaml:"delay"`
	AgentID         string `yaml:"agent_id"`
	MessageTemplate string `yaml:"message_template"`
}

type GitHubConfig struct {
	Secret          string `yaml:"secret"`
	NotifyMode      string `yaml:"notify_mode"` // "all" (default) or "failures"
	MessageTemplate string `yaml:"message_template"`
	AgentID         string `yaml:"agent_id"`
	Timeout         int    `yaml:"timeout"`
	Delay           int    `yaml:"delay"`
}

type AuditConfig struct {
	LogPath string `yaml:"log_path"`
}

var envRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

func envSubst(s string) string {
	return envRegex.ReplaceAllStringFunc(s, func(match string) string {
		key := envRegex.FindStringSubmatch(match)[1]
		if v := os.Getenv(key); v != "" {
			return v
		}
		return match
	})
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded := envSubst(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Gateway.AgentID == "" {
		cfg.Gateway.AgentID = "main"
	}
	if cfg.Audit.LogPath == "" {
		cfg.Audit.LogPath = "data/audit.log"
	}
	return &cfg, nil
}

// Validate checks config for common misconfigurations.
func (c *Config) Validate() error {
	hasRules := len(c.Trello.Rules) > 0 || c.GitHub.Secret != "" || c.Gmail.Enabled
	if hasRules && c.Gateway.URL == "" {
		return fmt.Errorf("gateway.url is required when trello/github/gmail rules are configured")
	}

	if c.Gmail.Enabled {
		allowedSet := make(map[string]bool, len(c.Google.AllowedEmails))
		for _, e := range c.Google.AllowedEmails {
			allowedSet[e] = true
		}
		for i, acc := range c.Gmail.Accounts {
			if acc.Email == "" {
				return fmt.Errorf("gmail.accounts[%d].email must not be empty", i)
			}
			if len(c.Google.AllowedEmails) > 0 && !allowedSet[acc.Email] {
				return fmt.Errorf("gmail.accounts[%d].email %q is not in google.allowed_emails", i, acc.Email)
			}
		}
	}

	if c.Server.InternalToken == "" {
		log.Println("Warning: server.internal_token is empty, /api/* routes are unprotected")
	}

	return nil
}

// ListIDToName returns the list name for a given list ID, or empty string.
func (c *Config) ListIDToName(id string) string {
	for name, lid := range c.Trello.Lists {
		if lid == id {
			return name
		}
	}
	return ""
}

// ResolvedAccounts returns Gmail account configs with inherited poll interval.
func (g GmailConfig) ResolvedAccounts() []GmailAccountConf {
	out := make([]GmailAccountConf, 0, len(g.Accounts))
	for _, a := range g.Accounts {
		if a.PollInterval == "" {
			a.PollInterval = g.PollInterval
		}
		out = append(out, a)
	}
	return out
}

// DefaultGitHubMessageTemplate returns the default template for GitHub events.
func DefaultGitHubMessageTemplate() string {
	return strings.TrimSpace(`
[Webhook Event] GitHub event detected.

Source: github
Event: {{.Event}}
Action: {{.Action}}
Repository: {{.Repository}}
PR: #{{.PRNumber}}
{{- if .PRTitle}}
Title: {{.PRTitle}}
{{- end}}
{{- if .Conclusion}}
Conclusion: {{.Conclusion}}
{{- end}}
`)
}
