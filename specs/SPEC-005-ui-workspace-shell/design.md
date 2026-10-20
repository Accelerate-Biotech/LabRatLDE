# SPEC-005 design: UI stack and workspace shell

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: React + TypeScript, built with Vite into `web/dist`, embedded with `go:embed` in a tiny `web` Go package and served at `/` by the existing `net/http` server. Data fetching uses plain `fetch` with a small typed client. Routing uses one router library. Lineage drawing uses one graph library. Plain CSS modules, no component framework.
- Not doing: server-side rendering, a state-management library, GraphQL, a separate UI server, or a design-system package.

## 2. Data and interfaces

- New package `web` (`web/web.go`, `//go:embed all:dist`), mounted by `api.Server.Handler` for any path outside `/v1/` and `/healthz`, with a fallback to `index.html`.
- `Makefile`: `ui` (npm ci and build), and `build` depends on `ui` when Node is available. CI builds the UI before `go build`.
- List endpoints return `{items:[…], next_cursor}` with `limit` ≤ 200.

## 3. UI

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

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | UI embedded in the server binary | Keeps the single-binary and air-gapped deployment (ROADMAP D8) | Separate static hosting or a Node server |
| 2026-10-18 | New dependencies (frontend only): `react`, `react-dom`, `react-router`, `typescript`, `vite`, a graph library (OQ 2), `markdown-it` (protocol rendering, HTML disabled), `@playwright/test` (dev) | Minimum set for the shell | A component framework or state library |
