# Domain Map

## Top-Level Layout

### `cmd/relay/`
- service entrypoint

### `internal/server/`
- bootstrap and wiring
- route registration
- background startup behavior

### `internal/config/`
- config structs
- YAML load and env substitution
- config validation

### `internal/webhook/`
- Trello webhook parsing + signature verification
- GitHub webhook parsing + signature verification

### `internal/auth/`
- Google OAuth flow
- bearer-token middleware for protected routes
- auth session handling

### `internal/gmail/`
- Gmail API client
- poller
- HTTP handlers for message/thread/label actions

### `internal/tokens/`
- encrypted token persistence
- token refresh persistence helpers

### `internal/gateway/`
- OpenClaw gateway client
- one-shot job dispatch payloads

### `internal/ratelimit/`
- per-event dedupe and TTL cleanup

### `internal/audit/`
- JSON-line request logging

## Config Surfaces

### Runtime config
- `config.yaml`

### Example config
- `config.yaml.example`

### Local secrets
- `.env`

Docs must reference env vars, not real values.

## Operational Docs

- `docs/configuration.md`
- `docs/webhooks.md`
- `docs/gmail-api.md`
- `docs/runbooks/ai-task-loop.md`
- `docs/runbooks/post-deploy-checklist.md`
- `docs/runbooks/incident-diagnosis.md`

## High-Risk Files

Changes here have elevated blast radius:
- `internal/config/config.go`
- `internal/auth/google.go`
- `internal/auth/middleware.go`
- `internal/tokens/store.go`
- `internal/gmail/poller.go`
- `internal/webhook/trello.go`
- `internal/webhook/github.go`
- `internal/gateway/client.go`

When touching these files:
- run full quality gate
- update docs if behavior changed
- call out migration or deploy implications explicitly
