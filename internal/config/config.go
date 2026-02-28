package config

import (
	"os"
	"regexp"

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
	Enabled      bool               `yaml:"enabled"`
	PollInterval string             `yaml:"poll_interval"`
	Rules        []GmailRule        `yaml:"rules"`    // legacy single-account mode
	Accounts     []GmailAccountConf `yaml:"accounts"` // multi-account mode
}

type GmailAccountConf struct {
	Email        string      `yaml:"email"`
	PollInterval string      `yaml:"poll_interval"`
	Rules        []GmailRule `yaml:"rules"`
}

type GmailRule struct {
	Name   string         `yaml:"name"`
	Match  GmailMatch     `yaml:"match"`
	Action GmailAction    `yaml:"action"`
}

type GmailMatch struct {
	From   []string `yaml:"from"`
	Labels []string `yaml:"labels"`
	Query  string   `yaml:"query"`
}

type GmailAction struct {
	Notify *GmailNotifyAction `yaml:"notify"`
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
}

type TrelloConfig struct {
	Secret string            `yaml:"secret"`
	Lists  map[string]string `yaml:"lists"`
	Rules  []TrelloRule      `yaml:"rules"`
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
	Secret     string `yaml:"secret"`
	NotifyMode string `yaml:"notify_mode"` // all | failures
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
	if cfg.GitHub.NotifyMode == "" {
		cfg.GitHub.NotifyMode = "all"
	}
	return &cfg, nil
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

// ResolvedAccounts returns Gmail account configs with legacy fallback.
func (g GmailConfig) ResolvedAccounts(allowedEmails []string) []GmailAccountConf {
	if len(g.Accounts) > 0 {
		out := make([]GmailAccountConf, 0, len(g.Accounts))
		for _, a := range g.Accounts {
			if a.PollInterval == "" {
				a.PollInterval = g.PollInterval
			}
			out = append(out, a)
		}
		return out
	}

	if len(g.Rules) == 0 {
		return nil
	}

	legacyEmail := ""
	if len(allowedEmails) == 1 {
		legacyEmail = allowedEmails[0]
	}
	return []GmailAccountConf{{
		Email:        legacyEmail,
		PollInterval: g.PollInterval,
		Rules:        g.Rules,
	}}
}
