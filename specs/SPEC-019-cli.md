# SPEC-019: CLI (optional)

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | none |
| **Depends on** | `SPEC-003`, `SPEC-006`, `SPEC-010`, `SPEC-012` |

## 1. Problem

Automation engineers and developers script LabRat from CI pipelines and instrument PCs. They push protocol versions from git, submit schema change requests (GitOps), upload files and verify audit chains. Doing all of that with `curl` means re-implementing OIDC flows and pagination in every script. Scientists use the web UI (ROADMAP D13), so the CLI is a convenience for this smaller group and is built last.

## 2. Scope

**In scope**
- Client subcommands on the existing `labrat` binary:
  - `login` (OIDC device code flow) and `logout`;
  - `protocol push <file.md> --experiment <slug>`;
  - `schema push <dir>` (submits change requests, SPEC-010 GitOps);
  - `upload <file> [--instrument] [--run] [--parser]`;
  - `stream <file.ndjson> --instrument [--run]`;
  - `audit export` and `audit verify`;
  - `get <resource> <id>`.
- Service credentials (client credentials grant) from the environment, for CI.
- `--json` output on every command, and documented exit codes.

**Out of scope**
- Anything the API doesn't offer (README design rule 1).
- Committing, signing or approving. Those stay human actions in the UI, although a human with the right role *may* commit through the CLI (open question 1).
- Shell completion and a TUI.

## 3. Simplicity budget

- Approach: subcommands in `cmd/server` using the standard `flag` package, a small HTTP client in the same package, and tokens cached in the user config directory with 0600 permissions.
- Not doing: a CLI framework (cobra), a generated SDK, or a separate binary.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-019-R01 | The CLI shall call only public `/v1` endpoints. | no | README design rule 1 |
| SPEC-019-R02 | When `labrat login` runs, the CLI shall authenticate with the OIDC device authorization grant and cache the tokens in a file readable only by the user. | no | security |
| SPEC-019-R03 | Where `LDE_CLIENT_ID` and `LDE_CLIENT_SECRET` are set, the CLI shall authenticate with the client credentials grant as a service principal. | no | CI use |
| SPEC-019-R04 | When `labrat schema push <dir>` runs, the CLI shall submit one change request per changed schema file, print each change request URL and the lint result, and exit non-zero if any lint fails. | no | GitOps |
| SPEC-019-R05 | When `labrat upload` or `labrat stream` completes, the CLI shall print the SHA-256 that LabRat recorded, and exit non-zero if it differs from the SHA-256 the CLI computed locally. | no | chain of custody |
| SPEC-019-R06 | Every CLI command shall support `--json` for machine-readable output and exit `0` on success, `1` on usage errors, `2` on API errors and `3` on verification failures. | no | scripting |

No `Test: yes` requirements. The CLI is a thin client; every rule it touches is enforced and tested server-side.

## 6. Data and interfaces

- Config: `LDE_URL`, `LDE_CLIENT_ID`, `LDE_CLIENT_SECRET`, `LDE_OIDC_ISSUER`. The token cache is `<user config dir>/labrat/tokens.json`.
- `labrat` with no subcommand, or `labrat serve`, keeps today's server behaviour.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should humans be able to commit change sets from the CLI, or only in the UI? Recommendation: UI only for v1; signing is never offered in the CLI because of re-authentication. | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Same binary, stdlib `flag` | One artifact to install and validate | `spf13/cobra`, a separate CLI binary |
| 2026-10-20 | The CLI is optional and built last (ROADMAP D13) | Scientists use the UI; the CLI serves automation | CLI-first |
