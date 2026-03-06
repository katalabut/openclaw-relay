# Post-Deploy Checklist

## Goal

Verify the relay is healthy after deploy or config change.

## Checks

1. Container/process is up.
2. `/health` returns OK.
3. Protected API still rejects missing token.
4. Audit log is writable.
5. Gmail auth state is visible if Gmail is enabled.
6. Expected webhook routes are reachable.

## Suggested Commands

```bash
docker compose ps
curl -fsS http://localhost:8080/health
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/status
```

Expected protected-route result without token: `401`.

## If Something Fails

- Check container logs first.
- Check env/config mismatch next.
- If Gmail auth broke after env or encryption-key changes, expect re-auth to be required.
- If webhook auth broke, re-check shared secret and proxy path.
