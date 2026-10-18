# SPEC-005: UI stack and workspace shell

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-18 |
| **Regulatory refs** | none |
| **Depends on** | `SPEC-001`, `SPEC-003` (protocol version views) |

## 1. Problem

LabRat's primary users are bench scientists, and today they can only reach it with `curl`. The design promises IDE-grade ergonomics, but no UI stack has been chosen. Without one, every later feature spec would end up API-only by default. This spec chooses the stack, delivers the workspace shell every later screen plugs into, and shows what the engine already does: experiments, runs, lineage and the audit trail.

## 2. Scope

**In scope**
- The UI stack decision and the build, embedded in the Go binary.
- The workspace shell: an explorer (left), tabbed editor pane (centre), and bottom panel with Problems and History tabs.
- Read-only views: experiment (rendered protocol, frontmatter, version list), run (state, params, inputs, outputs, pinned protocol, state history), lineage explorer (interactive DAG), audit viewer (paged events, verify button).
- Public API additions the views need: list experiments, list runs of an experiment, list instruments, and filter audit events by entity.
- A dev-only sign-in that sets the tenant and actor headers (replaced by SPEC-006).
- A Playwright harness wired into SPEC-004, with no tests yet.

**Out of scope**
- Any write action in the UI (creating runs, transitions, ingest). These come with their feature specs.
- Real authentication (SPEC-006).
- Reading charts (SPEC-011).
- Theming, localisation and a mobile layout.

## 3. Simplicity budget

- Approach: React + TypeScript, built with Vite into `web/dist`, embedded with `go:embed` in a tiny `web` Go package and served at `/` by the existing `net/http` server. Data fetching uses plain `fetch` with a small typed client. Routing uses one router library. Lineage drawing uses one graph library. Plain CSS modules, no component framework.
- Not doing: server-side rendering, a state-management library, GraphQL, a separate UI server, or a design-system package.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-005-R01 | The server shall serve the UI from assets embedded in the binary, with no runtime network access or separate web server. | no | air-gap (D8) |
| SPEC-005-R02 | If the UI assets were not built, then `go build` shall still succeed and `/` shall serve a page saying the UI is not built. | no | developer experience |
| SPEC-005-R03 | The UI shall call only documented `/v1` endpoints that any other client can use. | no | README design rule 1 |
| SPEC-005-R04 | The server shall send UI responses with a Content-Security-Policy that allows scripts, styles, images and connections only from its own origin. | no | security |
| SPEC-005-R05 | The workspace shall show an explorer listing experiments (with their runs) and instruments, and opening an item shall open it as a tab in the editor pane. | no | user need |
| SPEC-005-R06 | The run view shall show state, params, pinned protocol version and hash, inputs, generated datasets with reading count and SHA-256, and the run's state history from the audit trail. | no | user need |
| SPEC-005-R07 | The lineage explorer shall draw a node's closure as a left-to-right DAG with a direction toggle (upstream, downstream, both), and selecting a node shall open it. | no | user need |
| SPEC-005-R08 | The audit viewer shall page through events, filter by entity, and run `/v1/audit/verify`, showing whether the engine and database agree. | no | 11.10(e) visibility |
| SPEC-005-R09 | While `LDE_ENV=dev`, the UI shall offer a sign-in form for tenant and actor; otherwise it shall not show that form. | no | SPEC-001 out of scope (auth) |
| SPEC-005-R10 | The API shall offer `GET /v1/experiments`, `GET /v1/experiments/{id}/runs`, `GET /v1/instruments` and `?entity_type=&entity_id=` filters on `GET /v1/audit/events`, all cursor-paginated. | no | user need |
| SPEC-005-R11 | Every UI view shall be usable with the keyboard alone and meet WCAG 2.1 AA colour contrast. | no | accessibility |

No `Test: yes` requirements. This spec is UI plumbing; the business rules it displays are tested where they're enforced.

## 6. Data and interfaces

- New package `web` (`web/web.go`, `//go:embed all:dist`), mounted by `api.Server.Handler` for any path outside `/v1/` and `/healthz`, with a fallback to `index.html`.
- `Makefile`: `ui` (npm ci and build), and `build` depends on `ui` when Node is available. CI builds the UI before `go build`.
- List endpoints return `{items:[…], next_cursor}` with `limit` ≤ 200.

## 7. UI notes

```
┌ LabRat ───────────────────────────────────────────────────────────┐
│ Explorer        │ [Run: Plate 7 ×] [Lineage: Plate 7 ×]            │
│ ▾ il6-qpcr      │  state: signed   protocol: qpcr-ddct v3 (1c77…) │
│   • Plate 7     │  params …  inputs …  datasets …                 │
│ ▾ Instruments   │                                                 │
│   • HPLC-01     ├─────────────────────────────────────────────────┤
│                 │ Problems │ History                              │
└─────────────────┴─────────────────────────────────────────────────┘
```

- The Problems tab is empty until SPEC-009 and SPEC-016 fill it. The History tab shows audit events for the open entity until SPEC-007 replaces it with revisions.
- Target browsers: the current and previous major versions of Chrome, Edge and Firefox.

## 8. Non-functional

- The lineage explorer shall lay out and render a 500-node closure within 2 s on a 4-core laptop.
- The gzipped UI bundle shall be under 1 MB.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Confirm React + TypeScript + Vite. The README already names React; the alternative would be Svelte (smaller bundle, fewer contributors know it). | Rob Bassett | |
| 2 | Graph library: Cytoscape.js with the dagre layout (recommended: mature, one dependency for layout and rendering) or React Flow + elkjs? | Rob Bassett | |
| 3 | Is a VS Code–style "command palette" (Ctrl+K) wanted in v1, or later? | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | UI embedded in the server binary | Keeps the single-binary and air-gapped deployment (ROADMAP D8) | Separate static hosting or a Node server |
| 2026-10-18 | New dependencies (frontend only): `react`, `react-dom`, `react-router`, `typescript`, `vite`, a graph library (OQ 2), `markdown-it` (protocol rendering, HTML disabled), `@playwright/test` (dev) | Minimum set for the shell | A component framework or state library |
