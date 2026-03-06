# Quality Rules

## Repo Profile

- Repo class: `utility`
- Policy profile: `utility-strict`
- Default mode: PR-first, no silent auto-merge

## Required Commands

```bash
go mod download
gofmt -w .
go vet ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go build ./cmd/relay
```

## Required Outcomes

- `gofmt` clean
- `go vet` clean
- tests green
- coverage `>= 70%`
- build green

## Required Docs Sync

Update docs in the same change when behavior changes affect:
- config schema
- webhook event semantics
- Gmail OAuth / polling behavior
- token storage or auth requirements
- deploy or re-auth steps

Primary docs:
- `docs/configuration.md`
- `docs/webhooks.md`
- `docs/gmail-api.md`
- `docs/runbooks/*.md`

## Prohibited Shortcuts

- no live secrets in docs, examples, or committed config
- no weakening auth checks just to make local testing pass
- no skipping coverage gate for runtime changes
- no merging behavior-changing dispatch changes without matching tests

## Change-Specific Expectations

### Config changes
- update schema docs
- update example config if needed
- add or update config tests

### Webhook changes
- preserve signature verification
- preserve stale-event-safe behavior
- add or update parser/handler tests

### Gmail/auth changes
- preserve encrypted token storage
- preserve re-auth clarity in docs
- call out any migration or logout requirement

### Gateway dispatch changes
- keep prompts/messages narrow
- document any routing contract changes

## Delivery Checklist

- changed files are scoped
- tests and build results reported
- docs impact stated
- manual runtime steps called out
