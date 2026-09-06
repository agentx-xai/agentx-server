# Contributing

## Development

```bash
make test
make build
```

Rust code lives in `cli/`, Go code in `server/`, Vue code in `web/`, and reusable Agent context in `skills/`.

Keep adapters target-specific, do not copy credentials or sessions, and update `agentx-progress` when a planned capability changes state. New API routes must be documented in `server/openapi.yaml` and covered by tests.

## Pull requests

Describe the user-visible behavior, security implications, migration needs, and verification commands. Keep changes focused and preserve backward compatibility for manifest and lockfile formats.
