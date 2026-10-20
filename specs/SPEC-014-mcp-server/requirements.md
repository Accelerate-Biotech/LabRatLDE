# SPEC-014: MCP server

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(d), 11.10(e), 11.10(g), 11.100(a) |
| **Depends on** | `SPEC-006`, `SPEC-007`, `SPEC-009`, `SPEC-011` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

Scientists increasingly work with AI assistants (Claude, GPT) that could read protocols, explain lineage, draft schemas from a written protocol, or stage data. Without a standard interface, each integration means custom code against the REST API, and nothing stops an agent from doing what only a person may do. The Model Context Protocol (MCP) is the standard way to expose resources, tools and prompts to agents. LabRat should speak it natively, with accountability built in: agents act for a named human, can propose but never commit, and can never sign.

## 2. Scope

**In scope**
- An MCP endpoint (Streamable HTTP transport) at `/mcp` in the same binary, enabled by config.
- Authentication as an agent principal with an active delegation (SPEC-006).
- **Resources:** experiments with their current protocol text, protocol versions, runs (with signatures and manifest summary), lineage closures, schemas, and audit events for an entity.
- **Tools:**
  - read: `search_nodes`, `get_lineage`, `dataset_summary`;
  - check: `validate_against_schema`;
  - propose: `draft_schema` (in the delegating human's sandbox namespace) and `propose_change_set`;
  - later: `run_preflight` once SPEC-016 lands.
- **Prompts:** `summarize_run`, `draft_schema_from_protocol`, `review_ingest_proposal`.

**Out of scope**
- Any tool that commits, rejects, publishes, signs, transitions a run, grants roles or changes delegations.
- LabRat calling an LLM itself (ROADMAP D6: the engine stays LLM-free).
- The stdio transport, and MCP sampling or elicitation.
- Rate limiting (open question 2).

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-014-R01 | Where `LDE_MCP_ENABLED=true`, the server shall serve MCP over Streamable HTTP at `/mcp`; otherwise `/mcp` shall respond `404`. | no | deployment choice |
| SPEC-014-R02 | If an MCP request's bearer token doesn't resolve to an agent principal with an active delegation, then the MCP server shall reject it before any tool or resource runs. | yes | 11.10(d) |
| SPEC-014-R03 | The MCP server shall expose no tool that commits, rejects or withdraws change sets, publishes schemas, applies signatures, transitions runs, or changes principals, roles or delegations. | yes | ROADMAP D6, 11.100(a) |
| SPEC-014-R04 | When an MCP tool causes a mutation, the database shall record it with actor `<human> via <agent>` (SPEC-006-R09) in the same transaction. | yes | 11.10(e) |
| SPEC-014-R05 | While an agent acts for a human, the MCP server shall return only records that human's roles allow (SPEC-006-R08 intersection). | yes | 11.10(g) |
| SPEC-014-R06 | When `propose_change_set` is called, the MCP server shall create a change set with `source = agent` in state `proposed` and return its id and review URL. | no | workflow |
| SPEC-014-R07 | When `draft_schema` is called, the MCP server shall save a draft schema version in the delegating human's sandbox namespace (`~<handle>`) and never publish it. | no | Sandbox Rule |
| SPEC-014-R08 | When a tool fails validation, the MCP server shall return the same field errors as the REST API (SPEC-009-R05), as structured tool error content. | no | agent usability |
| SPEC-014-R09 | The MCP server shall describe every tool with a JSON input schema and a description stating whether it reads, checks or proposes. | no | agent usability |

## 4. Acceptance examples

Fixture: alice (scientist) delegates to agent `bot`, and `bot` holds scientist and reviewer. Run `R` exists in tenant `T1`.

**SPEC-014-R02**: an MCP `initialize` with alice's own (human) token fails. A `bot` token after alice ended the delegation fails.

**SPEC-014-R03**: `tools/list` returns no tool whose name or behaviour matches commit, reject, withdraw, publish, sign, transition, grant or delegate. Calling `propose_change_set`, then using the REST commit endpoint with `bot`'s token, fails with `LDE05` (SPEC-007-R06).

**SPEC-014-R04**: `propose_change_set` adding a note to `R` creates an audit event `change_set.proposed` with actor `alice via bot`.

**SPEC-014-R05**: reading tenant `T2` data is impossible (the token's tenant is `T1`). Reading a QA-only resource (if any is defined later) fails because alice lacks `qa`.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should agent read access to records be logged (not audited) for data-privacy reviews? Recommendation: a structured access log line per resource read, not an audit event. | Rob Bassett | |
| 2 | Is per-agent rate limiting needed in v1? | Rob Bassett | |
