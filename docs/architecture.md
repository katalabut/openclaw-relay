# Architecture

## Purpose

`openclaw-relay` is a narrow event-ingestion and dispatch service. It should stay simple:
- accept events
- validate authenticity
- normalize event data
- match rules
- dispatch a bounded OpenClaw job or notification

It should not absorb product-specific workflow logic that belongs in agents or repo docs elsewhere.

## High-Level Flow

```text
External event / poll
  -> HTTP handler or poller
  -> auth / signature verification
  -> config + rule matching
  -> rate limit / dedupe
  -> gateway client
  -> OpenClaw job or notify action
```

## Main Subsystems

### Server
- `cmd/relay/main.go`
- `internal/server/`

Loads config, initializes dependencies, mounts handlers, starts background pollers.

### Config
- `internal/config/`
- `config.yaml`
- `config.yaml.example`

Owns YAML parsing and `${VAR}` substitution.

### Webhooks
- `internal/webhook/`

Owns Trello/GitHub payload parsing and signature verification.

### Gmail
- `internal/gmail/`
- `internal/auth/`
- `internal/tokens/`

Owns OAuth login, token refresh, polling, and Gmail API endpoints.

### Gateway Dispatch
- `internal/gateway/`

Owns outbound calls to OpenClaw gateway.

### Audit + Rate Limit
- `internal/audit/`
- `internal/ratelimit/`

Owns request logging and duplicate-event suppression.

## Boundaries

### In scope
- validating inbound events
- dispatching narrow jobs
- exposing protected Gmail API helpers
- encrypted token persistence

### Out of scope
- Trello board workflow logic beyond event normalization
- product planning logic
- implementation logic for downstream repos
- long-lived business workflow state beyond relay-local persistence

## Key Invariants

- Public webhooks must be safe when replayed or duplicated.
- Protected API routes must require internal token auth.
- OAuth tokens must stay encrypted at rest.
- Config examples and docs must use env placeholders, not live secrets.
- Dispatch messages must stay narrow and reproducible.
