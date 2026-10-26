# SPEC-005: UI stack and workspace shell

| | |
|---|---|
| **Status** | Approved |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-26 |
| **Regulatory refs** | none |
| **Depends on** | `SPEC-001`, `SPEC-003` (protocol version views) |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

LabRat's primary users are bench scientists, and today they can only reach it with `curl`. The design promises IDE-grade ergonomics, but no UI stack has been chosen. Without one, every later feature spec would end up API-only by default. This spec chooses the stack, delivers the workspace shell every later screen plugs into, and shows what the engine already does: experiments, runs, lineage and the audit trail.

## 2. Scope

**In scope**
- The UI stack decision and the build, embedded in the Go binary.
- The workspace shell: an explorer (left), tabbed editor pane (centre), bottom panel with Problems and History tabs, and a command palette (Ctrl+K).
- Read-only views: experiment (rendered protocol, frontmatter, version list, diff of two versions), run (state, params, inputs, outputs, pinned protocol, state history), instrument, lineage explorer (interactive DAG), audit viewer (paged events, verify button).
- Public API additions the views need: list experiments, list runs of an experiment, list instruments, filter audit events by entity, run detail (inputs, datasets, pinned protocol hash), and `GET /v1/meta`.
- A dev-only sign-in that sets the tenant and actor headers (replaced by SPEC-006).
- A Playwright harness, with no tests yet. Its results join the SPEC-004 matrix in SPEC-008, which adds the first UI `Test: yes` requirement.

**Out of scope**
- Any write action in the UI (creating runs, transitions, ingest). These come with their feature specs.
- Real authentication (SPEC-006).
- Reading charts (SPEC-011).
- Opening a run, experiment or instrument view from a lineage node. It needs a node-to-entity lookup the API doesn't have yet.
- Theming, localisation and a mobile layout.

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-005-R01 | The server shall serve the UI from assets embedded in the binary, with no runtime network access or separate web server. | no | air-gap (D8) |
| SPEC-005-R02 | If the UI assets were not built, then `go build` shall still succeed and `/` shall serve a page saying the UI is not built. | no | developer experience |
| SPEC-005-R03 | The UI shall call only documented `/v1` endpoints that any other client can use. | no | README design rule 1 |
| SPEC-005-R04 | The server shall send UI responses with a Content-Security-Policy that allows scripts, styles, images and connections only from its own origin. | no | security |
| SPEC-005-R05 | The workspace shall show an explorer listing experiments (with their runs) and instruments, and opening an item shall open it as a tab in the editor pane. | no | user need |
| SPEC-005-R06 | The run view shall show state, params, pinned protocol version and hash, inputs, generated datasets with reading count and SHA-256, and the run's state history from the audit trail. | no | user need |
| SPEC-005-R07 | The lineage explorer shall draw a node's closure as a left-to-right DAG with a direction toggle (upstream, downstream, both), and selecting a node shall show its kind, label and attributes and offer to re-centre the graph on it. | no | user need |
| SPEC-005-R08 | The audit viewer shall page through events, filter by entity, and run `/v1/audit/verify`, showing whether the engine and database agree. | no | 11.10(e) visibility |
| SPEC-005-R09 | While `GET /v1/meta` reports `env` `dev`, the UI shall offer a sign-in form for tenant and actor; otherwise it shall not show that form. | no | SPEC-001 out of scope (auth) |
| SPEC-005-R10 | The API shall offer cursor-paginated `GET /v1/experiments`, `GET /v1/experiments/{id}/runs` and `GET /v1/instruments`, and `?entity_type=&entity_id=` filters on `GET /v1/audit/events`, which keeps its existing `after`/`limit` paging. | no | user need |
| SPEC-005-R11 | Every UI view shall be usable with the keyboard alone and meet WCAG 2.1 AA colour contrast. | no | accessibility |
| SPEC-005-R12 | When the user presses Ctrl+K (Cmd+K on macOS), the workspace shall open a command palette that filters experiments, runs, instruments and workspace commands by name, and Enter shall run or open the selected item. | no | OQ 3 |
| SPEC-005-R13 | `GET /v1/runs/{id}` shall also return the pinned protocol version number and SHA-256, the run's input nodes, and its generated datasets with reading count and SHA-256. | no | R06 |
| SPEC-005-R14 | The API shall offer `GET /v1/meta`, without identity headers, returning the server's environment (`dev` or `prod`). | no | R09, R03 |
| SPEC-005-R15 | The experiment view shall show a line diff between any two protocol versions of the experiment. | no | SPEC-003 design §UI |
| SPEC-005-R16 | The instrument view shall show the instrument's name, kind and attributes, and open its lineage. | no | R05 |

No `Test: yes` requirements. This spec is UI plumbing; the business rules it displays are tested where they're enforced.

## 4. Non-functional

- The lineage explorer shall lay out and render a 500-node closure within 2 s on a 4-core laptop.
- The gzipped UI bundle shall be under 1 MB.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Confirm React + TypeScript + Vite. The README already names React; the alternative would be Svelte (smaller bundle, fewer contributors know it). | Rob Bassett | Confirmed (2026-10-26) |
| 2 | Graph library: Cytoscape.js with the dagre layout (recommended: mature, one dependency for layout and rendering) or React Flow + elkjs? | Rob Bassett | Cytoscape.js + dagre (2026-10-26) |
| 3 | Is a VS Code–style "command palette" (Ctrl+K) wanted in v1, or later? | Rob Bassett | In v1 (2026-10-26): R12 |
