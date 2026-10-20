# SPEC-019 design: CLI (optional)

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: subcommands in `cmd/server` using the standard `flag` package, a small HTTP client in the same package, and tokens cached in the user config directory with 0600 permissions.
- Not doing: a CLI framework (cobra), a generated SDK, or a separate binary.

## 2. Data and interfaces

- Config: `LDE_URL`, `LDE_CLIENT_ID`, `LDE_CLIENT_SECRET`, `LDE_OIDC_ISSUER`. The token cache is `<user config dir>/labrat/tokens.json`.
- `labrat` with no subcommand, or `labrat serve`, keeps today's server behaviour.

## 3. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Same binary, stdlib `flag` | One artifact to install and validate | `spf13/cobra`, a separate CLI binary |
| 2026-10-20 | The CLI is optional and built last (ROADMAP D13) | Scientists use the UI; the CLI serves automation | CLI-first |
