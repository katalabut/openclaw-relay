# Gmail Integration Guide

## Google Cloud Console Setup

### 1. Create a Project

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Click **Select a project** → **New Project**
3. Name it (e.g., "openclaw-relay") and create

### 2. Enable Gmail API

1. **APIs & Services → Library**
2. Search for "Gmail API" → **Enable**

### 3. Configure OAuth Consent Screen

1. **APIs & Services → OAuth consent screen**
2. Choose **External** (or Internal if using Google Workspace)
3. Fill in app name, support email
4. Add scopes:
   - `https://www.googleapis.com/auth/gmail.modify`
   - `https://www.googleapis.com/auth/calendar.readonly`
   - `https://www.googleapis.com/auth/userinfo.email`
5. Add your email as a test user (required for External apps in testing mode)

### 4. Create OAuth Credentials

1. **APIs & Services → Credentials → Create Credentials → OAuth 2.0 Client ID**
2. Application type: **Web application**
3. Authorized redirect URI: `https://your-relay.example.com/auth/google/callback`
4. Copy the **Client ID** and **Client Secret**

### 5. Configure the Relay

Set in `.env`:

```env
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-client-secret
RELAY_ENCRYPTION_KEY=<64-char-hex-string>
```

Set in `config.yaml`:

```yaml
google:
  client_id: "${GOOGLE_CLIENT_ID}"
  client_secret: "${GOOGLE_CLIENT_SECRET}"
  redirect_url: "https://your-relay.example.com/auth/google/callback"
  allowed_emails:
    - "your-email@gmail.com"
```

## OAuth Flow

1. User visits `https://your-relay.example.com/` and clicks **Login with Google**
2. Redirect to `/auth/google/login` → generates a random state token → redirects to Google
3. User authorizes the app on Google's consent screen
4. Google redirects to `/auth/google/callback` with an authorization code
5. Relay exchanges the code for access + refresh tokens
6. Relay verifies the user's email against `allowed_emails`
7. Tokens are encrypted and saved to `data/tokens.json.enc`
8. User is redirected to `/` showing "✅ Authenticated as email@example.com"

To logout: visit `/auth/logout` (clears stored tokens).

The relay automatically refreshes expired access tokens using the stored refresh token.

## Polling Behavior

When `gmail.enabled: true`, the relay starts a background poller:

1. On first run, it calls `users.getProfile("me")` to get the initial `historyId`
2. State is persisted to `data/gmail-state.json`
3. Every `poll_interval` (default 60s), it calls `users.history.list` with `startHistoryId`
4. Only `messageAdded` history events are processed
5. For each new message, metadata is fetched (Subject, From headers)
6. Messages are evaluated against Gmail rules
7. The `historyId` is updated and saved after each poll

### History ID Expiration

If the stored `historyId` becomes too old (Google returns 404/notFound), the poller resets by fetching a fresh `historyId`. No messages are lost — they simply won't trigger rules for the gap period.

## Gmail Rules

### Match Fields

```yaml
gmail:
  rules:
    - name: "important-emails"
      match:
        labels: ["INBOX", "IMPORTANT"]  # AND: all must be present
        from: ["boss@company.com", "*@vip.com"]  # OR: any must match
      action:
        notify:
          target: "TELEGRAM_USER_ID"
          channel: "telegram"
          template: "🔥 {{.From}}: {{.Subject}}"
```

| Field | Logic | Description |
|-------|-------|-------------|
| `labels` | AND | All listed Gmail labels must be present on the message |
| `from` | OR | At least one pattern must match the From header (case-insensitive) |

**From pattern matching:**
- Exact substring: `user@example.com` matches if contained in the From header
- Suffix wildcard: `*@example.com` matches if From ends with `@example.com`

### Action Types

Currently one action type is supported:

#### `notify`

Sends a notification via the OpenClaw gateway (creates a one-shot job that sends a Telegram message).

| Field | Description |
|-------|-------------|
| `target` | Telegram user or chat ID |
| `channel` | Always `"telegram"` |
| `template` | Go template string |

**Template variables:**

| Variable | Description |
|----------|-------------|
| `{{.From}}` | Sender (From header) |
| `{{.Subject}}` | Email subject |
| `{{.Snippet}}` | Gmail snippet (preview text) |
| `{{.ID}}` | Gmail message ID |

## Token Security

### Encryption

OAuth tokens (access token, refresh token, email) are encrypted at rest using **AES-256-GCM**:

- Key: 32-byte key provided via `RELAY_ENCRYPTION_KEY` env var (64 hex characters)
- Nonce: Random 12 bytes generated per encryption operation
- Storage: `data/tokens.json.enc`

### Key Generation

```bash
openssl rand -hex 32
```

### Token Lifecycle

1. **Initial auth**: Access + refresh tokens obtained via OAuth code exchange
2. **Auto-refresh**: When the access token expires, the Gmail client automatically refreshes it using the refresh token and persists the new token
3. **Logout**: Visiting `/auth/logout` clears all stored tokens (deletes from encrypted store)

### Key Rotation

There is no automatic key migration. To rotate:

1. Stop the relay
2. Update `RELAY_ENCRYPTION_KEY` in `.env`
3. Delete `data/tokens.json.enc`
4. Restart and re-authenticate via the web UI
