# Incident Diagnosis

## Goal

Provide a deterministic first-pass debugging path for relay failures.

## Symptom -> First Check

### Service does not start
- inspect startup logs
- validate `config.yaml`
- validate required env vars

### `/health` fails
- confirm process/container is running
- inspect bind port and reverse proxy target

### Webhook accepted but no job dispatched
- inspect audit log and app logs
- confirm matching rule exists
- confirm rate limiter did not suppress duplicate event
- confirm gateway URL/token are valid

### GitHub or Trello webhook rejected
- re-check webhook secret
- inspect signature verification path
- confirm proxy preserved headers and body

### Gmail polling stopped
- inspect auth status
- inspect token refresh errors
- inspect saved polling state

### Gmail API routes return unauthorized
- verify `X-Relay-Token`
- verify internal token config

## Files To Inspect

- `config.yaml`
- `.env`
- `data/tokens.json.enc`
- `data/gmail-state.json`
- audit log path configured in `config.yaml`

## Recovery Rules

- Prefer identifying the broken boundary before changing config.
- If encryption key changed, assume token store is unreadable until re-auth.
- If webhook secret changed, assume all webhook verification failures are expected until sender config is updated.
- Do not change multiple auth surfaces at once during incident response.
