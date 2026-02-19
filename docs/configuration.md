# Configuration Reference

openclaw-relay is configured via a single `config.yaml` file. Environment variables are substituted using `${VAR}` syntax before YAML parsing.

## Environment Variable Substitution

Any value containing `${VAR_NAME}` is replaced with the corresponding environment variable at load time. If the variable is not set, the literal `${VAR_NAME}` is preserved.

```yaml
server:
  internal_token: "${RELAY_INTERNAL_TOKEN}"  # Replaced with env var value
```

## Full Config Schema

### `server`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | `8080` | HTTP listen port |
| `internal_token` | string | — | Bearer token for `/api/*` endpoint authentication. Checked via `X-Relay-Token` header. |

### `gateway`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | — | OpenClaw gateway base URL (e.g., `http://localhost:3777`) |
| `token` | string | — | Gateway bearer token for `/tools/invoke` |
| `agent_id` | string | `"work"` | Agent ID to receive dispatched jobs |

### `audit`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `log_path` | string | `"data/audit.log"` | Path to the JSON-line audit log file |

### `trello`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `secret` | string | — | HMAC secret for Trello webhook signature verification. If empty, signatures are not checked. |
| `lists` | map[string]string | — | Map of alias names to Trello list IDs. Used by the condition engine and for list ID → name resolution. |
| `rules` | []TrelloRule | — | List of event rules (see [YAML Rules Reference](../README.md#yaml-rules-reference)) |

### `trello.rules[*]`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `event` | string | — | `card_moved` or `comment_added` |
| `condition` | string | — | Condition expression (e.g., `list == 'ready'`) |
| `action.kind` | string | — | Job kind (`cron` for one-shot jobs) |
| `action.timeout` | int | `120` | Job timeout in seconds |
| `action.delay` | int | `2` | Seconds before the job fires |
| `action.message_template` | string | — | Go text/template for the agent message |

### `github`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `secret` | string | — | HMAC secret for GitHub webhook SHA-256 signature verification |

### `google`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `client_id` | string | — | Google OAuth 2.0 Client ID |
| `client_secret` | string | — | Google OAuth 2.0 Client Secret |
| `redirect_url` | string | — | OAuth callback URL (must match Google Console config) |
| `allowed_emails` | []string | — | Only these email addresses can authenticate via OAuth |

### `gmail`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable Gmail polling and API endpoints |
| `poll_interval` | string | `"60s"` | Polling frequency as a Go duration (`30s`, `2m`, etc.) |
| `rules` | []GmailRule | — | List of Gmail matching rules |

### `gmail.rules[*]`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | — | Human-readable rule name (used in logs) |
| `match.labels` | []string | — | All listed labels must be present (AND) |
| `match.from` | []string | — | At least one pattern must match (OR). Prefix `*` for suffix match. Case-insensitive. |
| `match.query` | string | — | Reserved for future use |
| `action.notify.target` | string | — | Telegram user/chat ID |
| `action.notify.channel` | string | — | Notification channel (e.g., `"telegram"`) |
| `action.notify.template` | string | `"📧 {{.From}}: {{.Subject}}"` | Go template for notification message |

## Full Annotated Example

```yaml
server:
  port: 8080
  internal_token: "${RELAY_INTERNAL_TOKEN}"

gateway:
  url: "${OPENCLAW_GATEWAY_URL}"
  token: "${OPENCLAW_GATEWAY_TOKEN}"
  agent_id: "work"

audit:
  log_path: "/data/audit.log"

trello:
  secret: "${TRELLO_WEBHOOK_SECRET}"
  lists:
    ready: "abc123"
    in_progress: "def456"
    done: "ghi789"
  rules:
    - event: card_moved
      condition: "list == 'ready'"
      action:
        kind: cron
        timeout: 300
        delay: 2
        message_template: |
          Card {{.CardName}} moved from {{.ListBeforeName}} to {{.ListAfterName}}.

github:
  secret: "${GITHUB_WEBHOOK_SECRET}"

google:
  client_id: "${GOOGLE_CLIENT_ID}"
  client_secret: "${GOOGLE_CLIENT_SECRET}"
  redirect_url: "https://your-relay.example.com/auth/google/callback"
  allowed_emails:
    - "you@example.com"

gmail:
  enabled: true
  poll_interval: 60s
  rules:
    - name: "inbox-notify"
      match:
        labels: ["INBOX"]
      action:
        notify:
          target: "YOUR_TELEGRAM_ID"
          channel: "telegram"
          template: "📧 {{.From}}: {{.Subject}}"
```

## Security Considerations

### Token Storage

OAuth tokens are stored encrypted on disk at `data/tokens.json.enc` using AES-256-GCM. The encryption key is provided via the `RELAY_ENCRYPTION_KEY` environment variable (64-character hex string = 32 bytes).

### Encryption Key Rotation

To rotate the encryption key:

1. Stop the relay
2. There is no built-in migration — you must re-authenticate via the Google OAuth flow
3. Set the new `RELAY_ENCRYPTION_KEY` in `.env`
4. Delete `data/tokens.json.enc`
5. Start the relay and visit the login page to re-authenticate

### Internal Token

The `server.internal_token` protects all `/api/*` endpoints. Public routes (`/webhook/*`, `/auth/*`, `/health`) are exempt from token checks.

### Webhook Secrets

- **Trello**: HMAC-SHA1 signature verified against `X-Trello-Webhook` header
- **GitHub**: HMAC-SHA256 signature verified against `X-Hub-Signature-256` header
- If the secret is empty, signature verification is skipped (not recommended for production)
