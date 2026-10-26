# SPEC-005 design: UI stack and workspace shell

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: React + TypeScript, built with Vite into `web/dist`. A tiny Go package `web` embeds it with `go:embed` and the existing `net/http` server mounts it at `/`. Data fetching uses plain `fetch` through one small typed client. Tabs live in React state, and the active tab is mirrored to `location.hash` (`#/run/<id>`), so links can be shared without a router library. The lineage graph uses Cytoscape.js with the dagre layout. Styling is plain CSS with a handful of colour tokens on `:root`. The diff is a small hand-written line LCS.
- No migrations. Every API addition reads existing tables.
- Not doing: server-side rendering, a router or state-management library, GraphQL, a separate UI server, a design-system or component package, or a diff library.

## 2. Data and interfaces

### Serving (R01, R02, R04)

- `web/web.go`, `package web`: `//go:embed all:dist` into `var dist embed.FS`, and `func Handler() http.Handler`.
  - `web/dist/.gitkeep` is committed, so the embed always matches something and `go build` works without Node (R02). `.gitignore` gets `/web/dist/*`, `!/web/dist/.gitkeep` and `/web/node_modules/`. The npm `build` script recreates `.gitkeep` after Vite empties the folder.
  - `GET`/`HEAD` only, otherwise 405. `/` serves `dist/index.html` with `Cache-Control: no-cache`. `/assets/*` serves hashed files with `Cache-Control: public, max-age=31536000, immutable`. Any other path returns 404 (hash routing means there is no SPA fallback).
  - If `dist/index.html` doesn't exist, `/` returns 200 `text/html` with a short page: "LabRat UI is not built. Run `make ui`, then rebuild the server." The API keeps working.
  - Every response sets `Content-Security-Policy: default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'` and `X-Content-Type-Options: nosniff`.
- `api.Server.Handler` registers `mux.Handle("/", web.Handler())`. It has **no method** in the pattern, because `GET /` would conflict with `/v1/` in Go 1.22's mux and panic at start-up.

### API additions (R10, R13, R14)

List endpoints take `?cursor=<uuid>&limit=<1..200>` (default 50) and return:

```json
{ "items": [ … ], "next_cursor": "<id of last item>" }
```

`next_cursor` is `null` when fewer than `limit` items came back. A malformed cursor or an out-of-range limit is 400. A cursor that names no row returns no items. Each query uses keyset pagination over a unique order. The cursor row is found with a subselect, e.g. `WHERE slug > (SELECT slug FROM experiments WHERE id = $1)`.

| Endpoint | Order | Item |
|---|---|---|
| `GET /v1/experiments` | `slug` | `{id, slug, title, created_at}` |
| `GET /v1/experiments/{id}/runs` | `created_at, id` | `{id, label, state, created_at}`, with `label` from the run's lineage node. 404 if the experiment doesn't exist. |
| `GET /v1/instruments` | `name` | `{id, node_id, name, kind, attrs, created_at}` |

`GET /v1/audit/events` gains `entity_type` and `entity_id`. They must be given together, or it's 400. With them, it returns only that entity's events, using the existing `audit_events_entity` index. Paging (`after`, `limit` ≤ 1000) and the bare-array response are unchanged, so existing clients keep working. The query lives in a new `audit.ScanEntity` next to `audit.Scan`, sharing its row decoding.

`GET /v1/runs/{id}` adds fields to the existing run object (additive, so existing clients aren't affected):

```json
{ "...run fields...": "",
  "protocol_version": 3, "protocol_sha256": "1c77…",
  "inputs":   [{"node_id": "…", "kind": "sample", "label": "PBMC donor 12"}],
  "datasets": [{"node_id": "…", "label": "HPLC-01 stream", "count": 5000, "sha256": "…"}] }
```

- `inputs` are `used` edges into the run's node (`to_id = run.node_id`), excluding the protocol node, ordered by label.
- `datasets` are `wasGeneratedBy` edges out of it (`from_id = run.node_id`), ordered by label. `count` and `sha256` come from that dataset's `instrument.ingested` audit payload (`payload::jsonb`), and are `null` if no such event exists. This is the hash pinned at ingest, so the view shows the chain-of-custody value rather than recomputing one.

`GET /v1/meta` → `{"env": "dev"}`. It's registered on the outer mux, **outside** `identify`, so the UI can read it before sign-in, and in prod, where `identify` refuses header identity.

### Build and delivery

- `web/` is the npm project: `package.json`, `package-lock.json`, `tsconfig.json`, `vite.config.ts`, `index.html`, `src/`, `e2e/`. It sits beside `web.go`. Dependencies are pinned to exact versions (no `^` or `~`), and the lockfile is committed.
- `Makefile`: `ui` runs `cd web && npm ci && npm run build`. `make build` stays Go-only; a binary built without `make ui` serves the R02 page. `e2e` runs `cd web && npx playwright test`.
- `Dockerfile`: a `node:22-alpine` stage builds `web/dist`, and the Go stage copies it in before `go build`, so images always carry the UI.
- CI: the `test` job runs `npm ci && npm run build` in `web/` before the Go steps, so `go vet` and `go test` see a built UI.
- `go list ./...` must not include anything under `web/node_modules`. None of the chosen packages ship `.go` files. If one ever does, move the npm project to a `_`-prefixed folder.

## 3. UI

```
┌ LabRat ──────────────────────────────────────── Ctrl+K ─ dev: alice ┐
│ Explorer        │ [Run: Plate 7 ×] [Lineage: Plate 7 ×]            │
│ ▾ il6-qpcr      │  state: signed   protocol: qpcr-ddct v3 (1c77…) │
│   • Plate 7     │  params …  inputs …  datasets …                 │
│ ▾ Instruments   │                                                 │
│   • HPLC-01     ├─────────────────────────────────────────────────┤
│ Audit trail     │ Problems │ History                              │
└─────────────────┴─────────────────────────────────────────────────┘
```

- **Sign-in (R09):** on load, the UI calls `GET /v1/meta`.
  - In `dev`, a form takes the tenant (prefilled `00000000-0000-4000-8000-000000000001`) and the actor, and stores them in `localStorage` (`labrat.tenant`, `labrat.actor`). The client sends them as `X-LDE-Tenant` and `X-LDE-Actor`. The header shows the actor and a sign-out button.
  - In `prod`, the UI shows "Sign-in is not configured yet" with no form.
- **Explorer (R05):** experiments expand to load their runs lazily. Instruments are listed, and an "Audit trail" item opens the audit viewer. Each list fetches its next page when you reach a "Load more" row. It's a `role="tree"` with arrow-key navigation; Enter opens a tab.
- **Tabs:** one tab per entity, keyed `kind:id`, so reopening one focuses it. Tabs follow the `role="tablist"` pattern, and Ctrl+W closes the active tab.
- **Experiment view (R05, R15):** shows the current protocol rendered with `markdown-it` (`html: false`), the frontmatter (`protocol_meta`) as a key/value table, and the version list. Choosing two versions shows a unified line diff (`src/diff.ts`, LCS over lines).
- **Run view (R06):** shows state, params, the pinned protocol version and hash, inputs, and datasets with count and SHA-256 from R13. State history comes from audit events filtered to `run`/`<id>`.
- **Instrument view (R16):** shows name, kind and attributes, with a button that opens the lineage tab of `node_id`.
- **Lineage explorer (R07):**
  - Cytoscape with `cytoscape-dagre`, `rankDir: 'LR'`. A segmented control (upstream, downstream, both) refetches `GET /v1/lineage/{id}?direction=`.
  - Clicking a node shows its kind, label, attributes and created time in a side panel, with a "Re-centre here" button.
  - The canvas isn't keyboard-accessible, so the side panel also lists every node as a listbox: arrow keys select and Enter re-centres (R11).
  - Run, experiment and instrument tabs have an "Open lineage" button for their node.
- **Audit viewer (R08):** a table of seq, time, actor, action, entity and reason, with "Next page" using `after=<last seq>`. Entity-type and entity-id fields apply the filter. "Verify chain" calls `/v1/audit/verify` and shows valid or broken, both heads, and whether engine and database agree.
- **Bottom panel:** the Problems tab is empty until SPEC-009 and SPEC-016 fill it. The History tab shows audit events for the active tab's entity (`experiment`, `run`, `instrument` or `lineage_node`) until SPEC-007 replaces it with revisions.
- **Command palette (R12):** Ctrl+K or Cmd+K opens a modal with a text box and a listbox. It filters, by case-insensitive substring, the explorer items already loaded plus the commands "Open audit trail", "Verify audit chain", "Close all tabs" and (dev) "Sign out". Arrow keys move, Enter runs, Esc closes and returns focus.
- **Accessibility (R11):** colour tokens are chosen for ≥ 4.5:1 text contrast. Every interactive element has a visible `:focus-visible` outline, and every icon-only button has an `aria-label`.
- **Critical Playwright flows:** none in this spec. The harness only.
- Target browsers: the current and previous major versions of Chrome, Edge and Firefox.

### Playwright harness

`@playwright/test` with `web/playwright.config.ts`:
- `testDir: 'e2e'`, `baseURL` from `LABRAT_URL` (default `http://localhost:8080`);
- reporter `[['list'], ['json', { outputFile: 'test-results/results.json' }]]`, Chromium only.

`npm run e2e` runs `playwright test --pass-with-no-tests`. `web/e2e/README.md` states the convention: every test cites its requirement as `// SPEC-NNN-Rxx` on the line above `test(`. SPEC-008 teaches `tools/validate` to read `results.json`.

## 4. Testing approach

No `Test: yes` requirements, so no tests. The Playwright harness ships empty.

## 5. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | UI embedded in the server binary | Keeps the single-binary and air-gapped deployment (ROADMAP D8) | Separate static hosting or a Node server |
| 2026-10-26 | React + TypeScript + Vite (OQ 1) | README already names React; most contributors know it | Svelte |
| 2026-10-26 | Cytoscape.js + `cytoscape-dagre` (OQ 2) | Mature, renders 500+ nodes on canvas, one plugin for layout | React Flow + elkjs (two dependencies, DOM-based rendering) |
| 2026-10-26 | New dependencies (frontend only): `react`, `react-dom`, `typescript`, `vite`, `@vitejs/plugin-react`, `cytoscape`, `cytoscape-dagre`, `markdown-it` (HTML disabled), `@types/react`, `@types/react-dom`, `@types/markdown-it`, `@playwright/test` (dev) | Minimum set for the shell. `@vitejs/plugin-react` is Vite's JSX transform | A component framework, state library or diff library |
| 2026-10-26 | No router: tabs in state, active tab in `location.hash` | Tabs, not pages, are the navigation model. Hash routing also removes the need for a server-side SPA fallback | `react-router` |
| 2026-10-26 | Audit events keep `after`/`limit` paging and an array response | Changing them would break existing clients and `make verify-chain` | Moving audit to the `{items, next_cursor}` shape |
| 2026-10-26 | `make build` doesn't build the UI | Keeps Go builds free of Node. Docker and CI build the UI explicitly | A conditional "build UI if Node is installed" target |
| 2026-10-26 | Lineage node selection shows details and re-centres, rather than opening the entity | The API has no node-to-entity lookup. Adding one is outside this spec | A `?node_id=` filter on runs and instruments |
