# AGENTS.md

This repository is a utility service. Optimize for reliability, diagnosability, and safe change control.

## First Read

Before changing code, read in this order:

1. `README.md`
2. `docs/architecture.md`
3. `docs/domain-map.md`
4. `docs/quality-rules.md`
5. Relevant runbook in `docs/runbooks/`

## What This Repo Does

`openclaw-relay` receives external events, validates them, matches rules, and dispatches narrow jobs to OpenClaw.

Current integrations:
- Trello webhooks
- GitHub webhooks
- Gmail OAuth + polling + API

## Source Of Truth

- Product overview and setup: `README.md`
- Runtime/config behavior: `docs/configuration.md`
- Integration behavior: `docs/webhooks.md`, `docs/gmail-api.md`
- Repo map: `docs/domain-map.md`
- Execution rules: `docs/quality-rules.md`
- Operational procedures: `docs/runbooks/`

If repo docs and chat context disagree, trust repo docs.

## Change Rules

- Keep secrets out of repo docs and config examples. Use `${VAR_NAME}` placeholders only.
- Do not weaken webhook verification, auth middleware, or token storage behavior without updating docs and tests.
- Treat dispatch behavior as contract-sensitive: changes to message templates, event mapping, or rate limiting need explicit validation.
- Prefer small, explicit changes over broad refactors in this repo.

## Required Checks

Run the repo quality gate before closing a task:

```bash
go mod download
gofmt -w .
go vet ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go build ./cmd/relay
```

Coverage floor: `70%`.

## Change Types That Require Extra Care

- `internal/config/*`
- `internal/auth/*`
- `internal/tokens/*`
- `internal/webhook/*`
- `internal/gateway/*`
- `internal/gmail/poller.go`
- `config.yaml.example`
- `docs/configuration.md`

For these areas, update docs and tests in the same change whenever behavior changes.

## Delivery Standard

Final report for this repo should include:
- files changed
- commands run
- coverage/build result
- config or runbook impact
- any manual deploy or re-auth step still required
