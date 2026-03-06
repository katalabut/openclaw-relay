# AI Task Loop

## Purpose

This runbook defines the default execution loop for AI agents working in this repo.

## Loop

1. Read `AGENTS.md` and the relevant docs.
2. Identify change type:
   - config
   - webhook
   - gmail/auth
   - gateway dispatch
   - ops/docs only
3. Inspect the smallest relevant subsystem first.
4. Make the change in the narrowest possible place.
5. Update tests and docs if behavior changed.
6. Run the full repo quality gate.
7. Report:
   - what changed
   - what was verified
   - whether deploy, restart, or re-auth is required

## Decision Rules

- If change touches auth, token storage, webhook verification, or deploy behavior: treat as strict.
- If config behavior changes: update `docs/configuration.md`.
- If event semantics change: update `docs/webhooks.md`.
- If Gmail/OAuth behavior changes: update `docs/gmail-api.md`.

## Escalate Instead Of Guessing

Escalate when any of the following is unclear:
- whether a change alters dispatch contract
- whether token/auth behavior remains backward compatible
- whether a deploy requires re-auth or data reset
