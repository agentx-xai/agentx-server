# Contributing to AgentX Server

Use the [organization contribution guide](https://github.com/agentx-xai/.github/blob/main/CONTRIBUTING.md) for the shared review and security rules.

## Local checks

```bash
go test ./...
go vet ./...
go build ./cmd/app
go build ./cmd/migrate
```

API changes must update [`openapi.yaml`](openapi.yaml) and include controller or use-case tests. Do not commit credentials, `.env` files, generated binaries, or private deployment data.
