# Contributing to openclaw-relay

## Project Overview

openclaw-relay is a Go service that bridges external webhooks (Trello, GitHub) and Gmail polling to an OpenClaw AI agent via the gateway API. It uses a YAML-based rules engine to evaluate events and dispatch one-shot agent jobs.

## Development Setup

### Prerequisites

- Go 1.24+
- Docker & Docker Compose (for integration testing)

### Getting Started

```bash
git clone https://github.com/katalabut/openclaw-relay.git
cd openclaw-relay
go mod download
go build ./cmd/relay
```

### Project Structure

```
cmd/relay/          — Entry point
internal/
  config/           — YAML config loading with env substitution
  server/           — HTTP server setup and route registration
  auth/             — Bearer token middleware + Google OAuth flow
  gateway/          — OpenClaw gateway client (job creation)
  webhook/          — Trello and GitHub webhook handlers
  gmail/            — Gmail API client, HTTP handlers, poller
  tokens/           — Encrypted token persistence (AES-256-GCM)
  ratelimit/        — Per-key rate limiter with TTL
  audit/            — JSON-line audit logging middleware
```

## Code Style

- Standard Go conventions: `gofmt`, `go vet`
- No external linter required; keep it simple
- Use `log.Printf` for logging (no external logging library)
- Interfaces where testability matters (see `GatewayClient`, `GmailClient`)

## Running Tests

```bash
go test ./... -v
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

### Coverage Requirement

**Minimum 70% coverage** — CI enforces this on every push and PR. Check locally before pushing:

```bash
COVERAGE=$(go tool cover -func=coverage.out | grep '^total' | awk '{print $3}' | tr -d '%')
echo "Coverage: $COVERAGE%"
```

## Pull Request Process

1. Fork the repo and create a feature branch
2. Write tests for new functionality
3. Ensure `go test ./...` passes with ≥ 70% coverage
4. Run `gofmt -w .` and `go vet ./...`
5. Open a PR with a clear description of what and why
6. One approval required before merge

## How to Add a New Webhook Source

Example: adding a **GitLab** webhook handler.

### Step 1: Add config types

In `internal/config/config.go`:

```go
type GitLabConfig struct {
    Secret string       `yaml:"secret"`
    Rules  []GitLabRule `yaml:"rules"`
}

type GitLabRule struct {
    Event     string     `yaml:"event"`
    Condition string     `yaml:"condition"`
    Action    RuleAction `yaml:"action"`
}
```

Add the field to `Config`:

```go
type Config struct {
    // ...existing fields...
    GitLab GitLabConfig `yaml:"gitlab"`
}
```

### Step 2: Create the handler

Create `internal/webhook/gitlab.go`:

```go
package webhook

type GitLabHandler struct {
    Config  *config.Config
    Gateway gateway.GatewayClient
    Limiter *ratelimit.Limiter
}

func (h *GitLabHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. Verify signature (X-Gitlab-Token header)
    // 2. Parse payload
    // 3. Rate-limit check
    // 4. Match rules
    // 5. Render template and create gateway job
}
```

### Step 3: Register the route

In `internal/server/server.go`:

```go
mux.Handle("/webhook/gitlab", &webhook.GitLabHandler{
    Config: cfg, Gateway: gw, Limiter: limiter,
})
```

### Step 4: Write tests

Create `internal/webhook/gitlab_test.go` with table-driven tests covering:
- Signature verification (valid, invalid, empty secret)
- Payload parsing
- Rate limiting
- Rule matching

### Step 5: Add to auth middleware

In `internal/auth/middleware.go`, `/webhook/` paths are already public — no changes needed.

## How to Add a New Internal API Endpoint

1. Create a handler function or method
2. Register it in the appropriate `RegisterRoutes` method or in `server.go`
3. All `/api/*` routes are automatically protected by the bearer token middleware
4. Return JSON via the `jsonResponse` / `jsonError` helpers in the gmail package (or write your own)
5. Write tests

## Issue Reporting

- Use GitHub Issues
- Include: Go version, OS, steps to reproduce, expected vs actual behavior
- For webhook issues: include the event type and (redacted) payload structure
- For config issues: include the relevant config section (redact secrets)
