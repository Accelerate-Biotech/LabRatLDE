# SPEC-014 design: MCP server

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: the official MCP Go SDK, mounted under `/mcp`. Each tool and resource is a thin adapter calling the same internal functions as the REST handlers, inside `WithTenant` with the agent principal. Every rule is already enforced below the adapter layer (SPEC-006/007/008), so MCP adds no permission logic of its own.
- Not doing: a separate MCP process, a tool registry framework, or agent memory and state in LabRat.

## 2. Data and interfaces

- Config: `LDE_MCP_ENABLED` (default `false`).
- Resource URIs: `lde://experiments/{id}`, `lde://protocol-versions/{id}`, `lde://runs/{id}`, `lde://lineage/{node}?direction=`, `lde://schemas/{ns}/{name}/{ver}`, `lde://audit/{entity_type}/{entity_id}`.
- The tools listed in §2 accept the same request shapes as their REST counterparts (SPEC-007, SPEC-009, SPEC-011).
- No new tables.

## 3. UI

- Change sets with `source = agent` appear in the review inbox labelled with the agent and the human it acted for.
- The Admin → Delegations screen (SPEC-006) shows each agent's last activity.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Dependency `github.com/modelcontextprotocol/go-sdk` | The official SDK tracks the protocol; hand-rolling JSON-RPC session handling isn't simpler | A hand-rolled implementation |
| 2026-10-20 | MCP is another transport over the same functions; no MCP-specific permissions | One enforcement point (the DB), so it can't drift | MCP-layer allowlists as the main control |
