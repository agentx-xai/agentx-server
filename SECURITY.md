# Security Policy

Do not commit API tokens, OAuth credentials, Agent sessions, `.env` files, or project source into an AgentX package.

Report vulnerabilities privately to the repository owner. Include a reproducible description, affected component, and proposed mitigation. Do not publish exploit details before a fix is available.

The server never executes Skill contents. Artifact uploads are size-limited, content-addressed, and checked for symlinks and path traversal. Hosted deployments must use OIDC and TLS; `AGENTX_API_TOKEN` is intended only for single-node deployments.
