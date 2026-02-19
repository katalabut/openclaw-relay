# Webhook Integration Guide

## Trello Webhooks

### How It Works

1. Trello sends a POST request to `/webhook/trello` when a board event occurs
2. The relay verifies the HMAC-SHA1 signature using `X-Trello-Webhook` header
3. The payload is parsed to identify the event type
4. The event is matched against configured rules
5. If matched, a one-shot agent job is created via the OpenClaw gateway

### Supported Event Types

| Trello Action | Relay Event | Condition |
|---------------|-------------|-----------|
| `updateCard` (with list change) | `card_moved` | Matched against `trello.lists` map |
| `commentCard` | `comment_added` | Card ID must be present |

Other Trello action types are silently ignored.

### Special Behaviors

- **HEAD requests**: Automatically return 200 OK (Trello uses this to verify callback URLs)
- **Questions list**: Card moves **to** the `questions` list are silently ignored (designed as a comment-only column)
- **Unwatched lists**: Moves to lists not in `trello.lists` are ignored

### Condition Syntax

Conditions compare the **list alias name** (from `trello.lists`) using simple equality:

```yaml
# Single list
condition: "list == 'ready'"

# Multiple lists (OR)
condition: "list == 'dev' || list == 'prod'"

# Match all (empty condition)
condition: ""
```

The alias name is resolved by looking up `listAfterID` in the `trello.lists` map.

### Template Variables

| Variable | Description |
|----------|-------------|
| `{{.CardID}}` | Trello card ID |
| `{{.CardName}}` | Card title |
| `{{.ListAfterID}}` | Destination list ID |
| `{{.ListAfterName}}` | Destination list display name (from Trello) |
| `{{.ListBeforeName}}` | Source list display name |
| `{{.ListName}}` | Same as `ListAfterName` |

### Action Configuration

```yaml
action:
  kind: cron         # Creates a one-shot scheduled job
  timeout: 300       # Agent job timeout in seconds (default: 120)
  delay: 2           # Seconds before the job fires (default: 2)
  message_template: |
    Card {{.CardName}} moved to {{.ListAfterName}}.
```

The job is created via the gateway's `/tools/invoke` endpoint as an `agentTurn` payload with the `cron` tool.

## GitHub Webhooks

### Supported Events

| GitHub Event | Trigger Condition |
|-------------|-------------------|
| `check_run` | `action == "completed"` |
| `workflow_run` | `action == "completed"` |
| `pull_request_review` | `action == "submitted"` |

All other events and non-matching actions are silently ignored.

### Payload Processing

The handler extracts:
- Repository full name
- PR number (from the event payload or associated pull requests)
- Event type and action

A fixed message template is used (not configurable via YAML rules). The agent receives the event type, action, repository, and PR number.

### Signature Verification

GitHub signs payloads with HMAC-SHA256. The relay checks the `X-Hub-Signature-256` header:

```
sha256=<hex-encoded-hmac>
```

If `github.secret` is empty, verification is skipped.

## Rules Engine

### How Rules Are Evaluated

**Trello:**
1. Parse the incoming webhook payload
2. Determine the event type (`card_moved` or `comment_added`)
3. Resolve the list alias from the list ID
4. Iterate through `trello.rules` in order
5. First rule matching both `event` and `condition` wins
6. Render the `message_template` with event data
7. Create a one-shot gateway job

**GitHub:**
GitHub uses a hardcoded message format (no configurable rules). The event is dispatched if it matches the supported event/action combinations.

### Template Rendering

Templates use Go's `text/template` syntax. Data is passed as a `map[string]string`.

```yaml
message_template: |
  Card {{.CardName}} ({{.CardID}}) moved from {{.ListBeforeName}} to {{.ListAfterName}}.
```

If template parsing or execution fails, the raw template string is used as fallback.

## Rate Limiting

The relay uses a per-key rate limiter with a **5-minute TTL**. Each event generates a key:

- Trello: `trello:<cardID>:<actionType>`
- GitHub: `github:<eventType>:<prNumber>`

If the same key was seen within the last 5 minutes, the event is silently dropped. This prevents duplicate processing when Trello or GitHub sends rapid-fire webhooks for the same event.

The limiter runs a background cleanup goroutine that purges expired entries every 10 minutes.

## Stale Event Guard

The relay dispatches events asynchronously via one-shot jobs. By the time the agent processes the job, the state may have changed (e.g., a card was moved again). The recommended pattern is to include a **stale event guard** in your message template:

```yaml
message_template: |
  Card {{.CardName}} moved to {{.ListAfterName}}.
  Expected List ID: {{.ListAfterID}}

  **STALE EVENT GUARD (MANDATORY FIRST STEP):**
  Verify the card is still in the expected column:
  GET /1/cards/{{.CardID}}?fields=idList
  If idList != {{.ListAfterID}} — exit silently.
```

This is a convention enforced in the agent prompt, not in the relay itself.
