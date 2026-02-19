# openclaw-relay

[![Test](https://github.com/katalabut/openclaw-relay/actions/workflows/test.yml/badge.svg)](https://github.com/katalabut/openclaw-relay/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.24-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A webhook relay service that bridges external events (Trello, GitHub, Gmail) to an [OpenClaw](https://openclaw.dev) AI agent. It receives webhooks and API polling data, evaluates them against configurable YAML rules, and dispatches one-shot agent jobs through the OpenClaw gateway.

## Architecture

```
┌──────────┐  ┌──────────┐  ┌──────────┐
│  Trello  │  │  GitHub  │  │  Gmail   │
│ Webhooks │  │ Webhooks │  │ (polled) │
└────┬─────┘  └────┬─────┘  └────┬─────┘
     │              │              │
     └──────────────┼──────────────┘
                    │
                    ▼
          ┌─────────────────┐
          │  openclaw-relay  │
          │                 │
          │  • Signature    │
          │    verification │
          │  • Rate limiter │
          │  • Rules engine │
          │  • Audit log    │
          │  • Gmail API    │
          └────────┬────────┘
                   │
                   ▼
          ┌─────────────────┐
          │ OpenClaw Gateway │
          │  /tools/invoke  │
          └────────┬────────┘
                   │
                   ▼
          ┌─────────────────┐
          │   AI Agent      │
          │  (one-shot job) │
          └─────────────────┘
```

## Features

- **Trello webhooks** — card moves and comments trigger agent jobs via configurable YAML rules
- **GitHub webhooks** — CI completions, PR reviews dispatched to agents
- **Gmail integration** — polls for new messages via History API, matches rules, sends notifications
- **YAML rules engine** — conditions, Go templates for message rendering
- **Rate limiting** — per-event deduplication with configurable TTL (5 min default)
- **HMAC signature verification** — Trello (SHA-1) and GitHub (SHA-256)
- **Google OAuth 2.0** — web-based login flow with allowed-email whitelist
- **Encrypted token storage** — AES-256-GCM for OAuth tokens at rest
- **Audit logging** — JSON-line request log with method, path, status, latency
- **Bearer token auth** — protects `/api/*` endpoints via `X-Relay-Token` header
- **Docker-ready** — multi-stage build, Traefik labels included

## Quick Start

### Prerequisites

- Docker & Docker Compose
- An OpenClaw gateway instance
- (Optional) Trello, GitHub, or Google API credentials

### Installation

```bash
git clone https://github.com/katalabut/openclaw-relay.git
cd openclaw-relay
```

### Configuration

**1. Create `.env`:**

```env
# Required
RELAY_INTERNAL_TOKEN=your-secret-api-token
OPENCLAW_GATEWAY_URL=http://host.docker.internal:3777
OPENCLAW_GATEWAY_TOKEN=your-gateway-token
RELAY_ENCRYPTION_KEY=64-char-hex-string-for-aes-256

# Trello (optional)
TRELLO_WEBHOOK_SECRET=your-trello-webhook-secret

# GitHub (optional)
GITHUB_WEBHOOK_SECRET=your-github-webhook-secret

# Google OAuth (optional, required for Gmail)
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-client-secret

# Traefik domain
RELAY_DOMAIN=your-relay.example.com
```

Generate an encryption key: `openssl rand -hex 32`

**2. Edit `config.yaml`** — see [Configuration Reference](#configuration-reference) below.

### Running

```bash
docker compose up -d
```

Verify health:

```bash
curl https://your-relay.example.com/health
# {"status":"ok"}
```

## Configuration Reference

```yaml
# Server settings
server:
  port: 8080                              # Listen port (default: 8080)
  internal_token: "${RELAY_INTERNAL_TOKEN}" # Bearer token for /api/* routes

# OpenClaw gateway connection
gateway:
  url: "${OPENCLAW_GATEWAY_URL}"          # Gateway base URL
  token: "${OPENCLAW_GATEWAY_TOKEN}"      # Gateway auth token
  agent_id: "work"                        # Agent to receive jobs (default: "work")

# Audit log
audit:
  log_path: "/data/audit.log"             # Path to JSON audit log (default: "data/audit.log")

# Trello webhook configuration
trello:
  secret: "${TRELLO_WEBHOOK_SECRET}"      # HMAC secret for signature verification
  lists:                                   # Map of list aliases → Trello list IDs
    ready: "LIST_ID_HERE"
    in_progress: "LIST_ID_HERE"
  rules:                                   # See "YAML Rules Reference" below
    - event: card_moved
      condition: "list == 'ready'"
      action:
        kind: cron
        timeout: 300                       # Job timeout in seconds
        delay: 2                           # Seconds before job fires
        message_template: |
          Card {{.CardName}} moved to {{.ListAfterName}}

# GitHub webhook configuration
github:
  secret: "${GITHUB_WEBHOOK_SECRET}"      # HMAC secret for SHA-256 verification

# Google OAuth (required for Gmail)
google:
  client_id: "${GOOGLE_CLIENT_ID}"
  client_secret: "${GOOGLE_CLIENT_SECRET}"
  redirect_url: "https://your-relay.example.com/auth/google/callback"
  allowed_emails:                          # Only these emails can authenticate
    - "user@example.com"

# Gmail integration
gmail:
  enabled: true                            # Enable Gmail polling
  poll_interval: 60s                       # Polling frequency (Go duration)
  rules:
    - name: "new-inbox-message"
      match:
        labels: ["INBOX"]                  # All listed labels must be present
        from: ["*@example.com"]            # Prefix with * for suffix match
        query: ""                          # Gmail search query (unused in polling)
      action:
        notify:
          target: "USER_ID"               # Telegram user/chat ID
          channel: "telegram"
          template: "📧 {{.From}}: {{.Subject}}"
```

Environment variables use `${VAR}` syntax and are substituted at load time.

## Webhook Setup

### Trello

Register a webhook using the Trello API:

```bash
curl -X POST "https://api.trello.com/1/webhooks" \
  -H "Content-Type: application/json" \
  -d '{
    "callbackURL": "https://your-relay.example.com/webhook/trello",
    "idModel": "YOUR_BOARD_ID",
    "description": "openclaw-relay",
    "key": "YOUR_TRELLO_API_KEY",
    "token": "YOUR_TRELLO_TOKEN"
  }'
```

Trello sends a HEAD request to verify the callback URL, which the relay accepts automatically.

### GitHub

In your repository: **Settings → Webhooks → Add webhook**

| Field | Value |
|-------|-------|
| Payload URL | `https://your-relay.example.com/webhook/github` |
| Content type | `application/json` |
| Secret | Same as `GITHUB_WEBHOOK_SECRET` |
| Events | Select: Check runs, Workflow runs, Pull request reviews |

## API Reference

All `/api/*` endpoints require the `X-Relay-Token` header (except `/health`).

### Health Check

```bash
curl https://your-relay.example.com/health
# {"status":"ok"}
```

### Service Status

```bash
curl -H "X-Relay-Token: YOUR_TOKEN" \
  https://your-relay.example.com/api/status
# {"status":"ok","service":"openclaw-relay"}
```

### Auth Status

```bash
curl -H "X-Relay-Token: YOUR_TOKEN" \
  https://your-relay.example.com/api/auth/status
# {"google":{"authenticated":true,"email":"user@example.com","expires_at":"..."}}
```

### List Gmail Messages

```bash
curl -H "X-Relay-Token: YOUR_TOKEN" \
  "https://your-relay.example.com/api/gmail/messages?q=is:unread&max=10"
```

Query parameters:
- `q` — Gmail search query (default: `is:unread`)
- `max` — Max results (default: `20`)

### Get Gmail Message

```bash
curl -H "X-Relay-Token: YOUR_TOKEN" \
  https://your-relay.example.com/api/gmail/message/MESSAGE_ID
```

### Modify Gmail Message

```bash
curl -X POST -H "X-Relay-Token: YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  https://your-relay.example.com/api/gmail/modify/MESSAGE_ID \
  -d '{
    "addLabels": ["STARRED"],
    "removeLabels": ["UNREAD"],
    "archive": true,
    "markRead": true,
    "star": false
  }'
```

### List Gmail Labels

```bash
curl -H "X-Relay-Token: YOUR_TOKEN" \
  https://your-relay.example.com/api/gmail/labels
```

### Get Gmail Thread

```bash
curl -H "X-Relay-Token: YOUR_TOKEN" \
  https://your-relay.example.com/api/gmail/threads/THREAD_ID
```

## Google OAuth Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project (or select existing)
3. **APIs & Services → Library** → Enable **Gmail API**
4. **APIs & Services → OAuth consent screen** → Configure (External or Internal)
   - Add scopes: `gmail.modify`, `calendar.readonly`, `userinfo.email`
5. **APIs & Services → Credentials → Create Credentials → OAuth 2.0 Client ID**
   - Application type: **Web application**
   - Authorized redirect URI: `https://your-relay.example.com/auth/google/callback`
6. Copy Client ID and Client Secret to your `.env`
7. Deploy the relay, then visit `https://your-relay.example.com/` and click **Login with Google**
8. Only emails listed in `google.allowed_emails` will be accepted

## YAML Rules Reference

### Trello Rules

Each rule has three fields:

| Field | Description |
|-------|-------------|
| `event` | Event type: `card_moved` or `comment_added` |
| `condition` | Expression like `list == 'ready'` or `list == 'dev' \|\| list == 'prod'` |
| `action` | Job configuration (see below) |

**Condition syntax:** Simple equality checks on the list alias name. Supports `||` (OR) for multiple lists. Empty condition matches all.

**Action fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `kind` | string | — | Job type (`cron` for one-shot) |
| `timeout` | int | 120 | Job timeout in seconds |
| `delay` | int | 2 | Seconds before job fires |
| `message_template` | string | — | Go template for the agent message |

**Template variables for Trello:**

| Variable | Description |
|----------|-------------|
| `{{.CardID}}` | Trello card ID |
| `{{.CardName}}` | Card title |
| `{{.ListAfterID}}` | Destination list ID |
| `{{.ListAfterName}}` | Destination list name |
| `{{.ListBeforeName}}` | Source list name |
| `{{.ListName}}` | Same as ListAfterName |

**Supported Trello action types:**
- `updateCard` (with list change) → `card_moved` event
- `commentCard` → `comment_added` event

Note: Card moves **to** the `questions` list are silently ignored (comment-only column).

### Gmail Rules

```yaml
gmail:
  rules:
    - name: "rule-name"
      match:
        labels: ["INBOX"]          # ALL listed labels must be present
        from: ["user@example.com"] # ANY listed pattern must match (case-insensitive)
      action:
        notify:
          target: "CHAT_ID"
          channel: "telegram"
          template: "📧 {{.From}}: {{.Subject}}"
```

**Match fields:**
- `labels` — All specified labels must be present on the message (AND logic)
- `from` — At least one pattern must match (OR logic). Prefix with `*` for suffix matching (e.g., `*@company.com`)

**Notify template variables:** `{{.From}}`, `{{.Subject}}`, `{{.Snippet}}`, `{{.ID}}`

## Development

### Run Locally

```bash
# Set environment variables
export RELAY_INTERNAL_TOKEN=dev-token
export OPENCLAW_GATEWAY_URL=http://localhost:3777
export OPENCLAW_GATEWAY_TOKEN=dev-gateway-token

go run ./cmd/relay -config config.yaml
```

### Run Tests

```bash
go test ./... -v
```

### Check Coverage

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
# CI requires ≥ 70% coverage
```

## License

MIT
