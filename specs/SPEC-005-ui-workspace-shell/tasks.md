# SPEC-005 tasks: UI stack and workspace shell

Requirements: [requirements.md](requirements.md) · Design: [design.md](design.md)

No migrations, no schema changes, and no tests: the spec has no `Test: yes` requirement. Run `docker compose up -d` and `go run ./cmd/server` for any task that verifies against the API. The dev headers below are `-H 'X-LDE-Tenant: 00000000-0000-4000-8000-000000000001' -H 'X-LDE-Actor: dev'`. Every task ends with `gofmt -l . && go vet ./...` clean. Phase 3 and 4 tasks also end with `cd web && npm run build` clean (it runs `tsc`).

## Phase 1: API

- [x] 1.1 Audit events: filter by entity
  - Requirements: SPEC-005-R10
  - Files: `internal/audit/postgres.go`, `internal/api/audit.go`
  - Do:
    - In `postgres.go`, move the row loop of `Scan` into an unexported `scanRows(rows pgx.Rows, fn func(Event) error) error`. `Scan` keeps its signature and behaviour.
    - Add `func ScanEntity(ctx context.Context, tx pgx.Tx, tenantID, entityType, entityID string, afterSeq int64, limit int, fn func(Event) error) error`. It runs the same `SELECT` columns as `Scan` with `WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3 AND seq > $4 ORDER BY seq LIMIT $5`, then `scanRows`.
    - In `listAuditEvents`, read `entity_type` and `entity_id`. If exactly one is non-empty, return `badRequest("entity_type and entity_id must be given together")`. If both are given, call `ScanEntity`, otherwise `Scan`. Paging and the response are unchanged. Update the doc comment to `GET /v1/audit/events?after=<seq>&limit=<n≤1000>[&entity_type=&entity_id=]`.
    - Don't touch `Digest`, `Append`, `VerifyTenant` or any migration.
  - Verify: `make test-integration` passes. Create an experiment, then `curl '…/v1/audit/events?entity_type=experiment&entity_id=<id>'` returns only its `experiment.created` event. Giving only `entity_type` is 400. `make verify-chain` still passes.
  - Integrity: yes
- [x] 1.2 Page helper and `GET /v1/experiments`
  - Requirements: SPEC-005-R10
  - Files: `internal/api/server.go`, `internal/api/experiments.go`
  - Do:
    - In `server.go`, add:
      - `type page struct { Items any \`json:"items"\`; NextCursor *string \`json:"next_cursor"\` }`;
      - `func readPage(r *http.Request) (cursor string, limit int, err error)`: `limit` defaults to 50, and anything that isn't an integer in 1..200 is `badRequest("limit must be 1..200")`. A non-empty `cursor` that isn't a UUID is `badRequest("cursor must be a uuid")`;
      - `func nextCursor(n, limit int, lastID string) *string`: returns `&lastID` when `n == limit`, else `nil`.
    - In `experiments.go`, add `listExperiments` and register `v1.HandleFunc("GET /v1/experiments", s.listExperiments)` in `server.go`. Inside `WithTenant`, run `SELECT id, slug, title, created_at FROM experiments WHERE ($1 = '' OR slug > (SELECT slug FROM experiments WHERE id = NULLIF($1,'')::uuid)) ORDER BY slug LIMIT $2`, passing the cursor as text. Items are `{id, slug, title, created_at}`. Respond `page{Items: items, NextCursor: …}`, with `items` never `nil` (`[]`).
  - Verify: create 3 experiments. `GET /v1/experiments?limit=2` returns 2 and a `next_cursor`, and following it returns 1 and `next_cursor: null`. `limit=0` and `cursor=x` are 400.
- [x] 1.3 `GET /v1/experiments/{id}/runs` and `GET /v1/instruments`
  - Requirements: SPEC-005-R10
  - Files: `internal/api/runs.go`, `internal/api/instruments.go`, `internal/api/server.go` (routes only)
  - Do:
    - `listExperimentRuns`: a non-UUID `id` is `notFound("experiment")`. Inside `WithTenant`, first check `SELECT 1 FROM experiments WHERE id = $1`, where no row is `notFound("experiment")`. Then:
      `SELECT r.id, n.label, r.state, r.created_at FROM runs r JOIN lineage_nodes n ON n.id = r.node_id WHERE r.experiment_id = $1 AND ($2 = '' OR (r.created_at, r.id) > (SELECT created_at, id FROM runs WHERE id = NULLIF($2,'')::uuid)) ORDER BY r.created_at, r.id LIMIT $3`.
      Items are `{id, label, state, created_at}`.
    - `listInstruments`: `SELECT id, node_id, name, kind, attrs, created_at FROM instruments WHERE ($1 = '' OR name > (SELECT name FROM instruments WHERE id = NULLIF($1,'')::uuid)) ORDER BY name LIMIT $2`.
    - Both use `readPage`, `nextCursor` and `page` from 1.2. Register `GET /v1/experiments/{id}/runs` and `GET /v1/instruments`.
  - Verify: with two runs on one experiment, `…/runs?limit=1` pages through both in creation order. An unknown experiment is 404. `GET /v1/instruments` lists registered instruments by name.
- [x] 1.4 Run detail fields
  - Requirements: SPEC-005-R13, SPEC-005-R06
  - Files: `internal/api/runs.go`
  - Do:
    - Add types:
      - `runInput {NodeID, Kind, Label string}`, JSON `node_id`, `kind`, `label`;
      - `runDataset {NodeID, Label string; Count *int64; SHA256 *string}`, JSON `node_id`, `label`, `count`, `sha256`;
      - `runDetail struct { domain.Run; ProtocolVersion int \`json:"protocol_version"\`; ProtocolSHA256 string \`json:"protocol_sha256"\`; Inputs []runInput \`json:"inputs"\`; Datasets []runDataset \`json:"datasets"\` }`.
    - In `getRun`, after loading the run, in the same `WithTenant` run three queries:
      - `SELECT version, encode(sha256,'hex') FROM protocol_versions WHERE id = $1` with the run's `ProtocolVersionID`;
      - `SELECT n.id, n.kind, n.label FROM lineage_edges e JOIN lineage_nodes n ON n.id = e.from_id WHERE e.to_id = $1 AND e.relation = 'used' AND n.kind <> 'protocol' ORDER BY n.label` with the run's `NodeID`;
      - `SELECT n.id, n.label, (a.payload::jsonb->>'count')::bigint, a.payload::jsonb->>'sha256' FROM lineage_edges e JOIN lineage_nodes n ON n.id = e.to_id LEFT JOIN audit_events a ON a.entity_type = 'dataset' AND a.entity_id = n.id::text AND a.action = 'instrument.ingested' WHERE e.from_id = $1 AND e.relation = 'wasGeneratedBy' ORDER BY n.label`.
    - `Inputs` and `Datasets` are `[]` when there are none, never `null`. Respond with `runDetail`. `createRun` and `transitionRun` responses are unchanged.
  - Verify: create a sample node, a run using it, and an instrument, then stream 3 readings with `run_id`. `GET /v1/runs/{id}` shows the sample in `inputs` (and no protocol node), and the dataset with `count: 3` and the same `sha256` the stream response returned.
- [x] 1.5 `GET /v1/meta`
  - Requirements: SPEC-005-R14
  - Files: `internal/api/server.go`
  - Do: add `func (s *Server) meta(w, r)` writing `{"env": s.cfg.Env}`. Register it on the **outer** `mux` with `mux.HandleFunc("GET /v1/meta", s.meta)`, so it isn't wrapped by `identify`. Add a comment saying why.
  - Verify: `curl localhost:8080/v1/meta` with no headers returns `{"env":"dev"}`. With `LDE_ENV=prod`, it returns `{"env":"prod"}` while other `/v1` calls return 501.

## Phase 2: Serving and build

- [x] 2.1 `web` package: embedded assets, CSP, not-built page
  - Requirements: SPEC-005-R01, SPEC-005-R02, SPEC-005-R04
  - Files: `web/web.go` (new), `web/dist/.gitkeep` (new, empty), `.gitignore`, `internal/api/server.go`
  - Do:
    - `web.go`, `package web`: `//go:embed all:dist` → `var dist embed.FS`. Add `func Handler() http.Handler`, implemented exactly as in `design.md` §2 "Serving": the method check (405 with `Allow: GET, HEAD`); the security headers on every response, including 404 and 405; `/` → `index.html` with `no-cache`, or the not-built page when `fs.Stat(dist, "dist/index.html")` fails; `/assets/<file>` via `http.FileServerFS` on `fs.Sub(dist, "dist")` with the immutable cache header; everything else 404.
    - Keep the not-built page as a Go string constant, with no inline script or style.
    - `.gitignore`: add `/web/dist/*`, `!/web/dist/.gitkeep`, `/web/node_modules/`, `/web/test-results/`, `/web/playwright-report/`.
    - In `api.Server.Handler`, add `mux.Handle("/", web.Handler())`. There must be no method in the pattern (see design).
  - Verify: `go build ./...` succeeds with only `.gitkeep` in `web/dist`. `curl -i localhost:8080/` shows the not-built page and the CSP header. `curl -i -X POST localhost:8080/` is 405. `/healthz` and `/v1/...` behave as before.
- [x] 2.2 Frontend scaffold
  - Requirements: SPEC-005-R01, SPEC-005-R03
  - Files (all new under `web/`): `package.json`, `package-lock.json`, `tsconfig.json`, `vite.config.ts`, `index.html`, `src/main.tsx`, `src/App.tsx`, `src/styles.css`
  - Do:
    - `npm init` in `web/`, `"private": true`, `"type": "module"`.
    - Install the runtime and dev dependencies named in the design's decision log, except `@playwright/test`, which comes in 4.1. Use `npm install --save-exact` with the current stable versions.
    - Scripts:
      - `"build": "tsc -b && vite build && node -e \"require('fs').writeFileSync('dist/.gitkeep','')\""`;
      - `"dev": "vite"`.
    - `vite.config.ts`: the React plugin, `build.outDir: 'dist'`, `build.assetsDir: 'assets'`. Add a `server.proxy` sending `/v1` to `http://localhost:8080`, for development only.
    - `tsconfig.json`: `strict: true`, `jsx: react-jsx`, `noEmit: true`, bundler module resolution.
    - `index.html`: no inline script or style. `<title>LabRat</title>`.
    - `App.tsx`: renders "LabRat" only for now.
    - `styles.css`: colour tokens on `:root` (`--bg`, `--fg`, `--muted`, `--accent`, `--border`, `--danger`, `--ok`), chosen for ≥ 4.5:1 contrast on `--bg`, plus a global `:focus-visible` outline.
  - Verify: `npm run build` produces `web/dist/index.html`, `web/dist/assets/*` and `web/dist/.gitkeep`. After `go run ./cmd/server`, `/` shows "LabRat" with no CSP errors in the browser console. `go list ./... | grep node_modules` prints nothing.
- [x] 2.3 Makefile, Dockerfile, CI
  - Requirements: SPEC-005-R01
  - Files: `Makefile`, `Dockerfile`, `.github/workflows/ci.yml`, `.dockerignore` (create if absent)
  - Do:
    - `Makefile`: add `ui` (`cd web && npm ci && npm run build`) and `e2e` (`cd web && npx playwright test`) to `.PHONY` with `##` help comments. `build` is unchanged.
    - `Dockerfile`: add a first stage `FROM node:22-alpine AS ui`, `WORKDIR /web`. Copy `web/package.json` and `web/package-lock.json`, `npm ci`, then copy `web/` and `npm run build`. In the Go stage, `COPY --from=ui /web/dist ./web/dist` after `COPY . .`.
    - `.dockerignore`: `web/node_modules`, `web/dist`, `bin`, `.claude`.
    - `ci.yml` `test` job, before the gofmt step: `actions/setup-node@v4` with `node-version: 22`, `cache: npm`, `cache-dependency-path: web/package-lock.json`, then `npm ci && npm run build` with `working-directory: web`.
  - Verify: `make ui && make build && ./bin/labrat` serves the UI. `docker build -t labrat:ui .` succeeds, and the container serves `/` with the UI, not the not-built page.

## Phase 3: UI

Each view lives in one file under `web/src/`. Only the endpoints in `src/api.ts` may be called (R03).

- [x] 3.1 API client and dev sign-in
  - Requirements: SPEC-005-R03, SPEC-005-R09
  - Files: `web/src/api.ts`, `web/src/SignIn.tsx`, `web/src/App.tsx`
  - Do:
    - `api.ts`:
      - `async function get<T>(path: string): Promise<T>` adds `X-LDE-Tenant` and `X-LDE-Actor` from `localStorage` (`labrat.tenant`, `labrat.actor`, each read in try/catch), and throws `Error(body.error ?? status)` on non-2xx;
      - typed functions for every endpoint the UI uses: `meta`, `listExperiments`, `getExperiment`, `listProtocolVersions`, `getProtocolVersion`, `listRuns`, `getRun`, `listInstruments`, `getLineage`, `listAuditEvents(after, entityType?, entityId?)`, `verifyAudit`;
      - TypeScript types matching the server JSON. Read the Go handlers for the field names.
    - `App.tsx`: on load, call `meta()`.
      - For `prod`, show "Sign-in is not configured yet (SPEC-006)."
      - For `dev` without a stored actor, show `SignIn`: a form with tenant (prefilled with the dev tenant) and actor, and labelled inputs, where submit stores both. Otherwise render the workspace placeholder and a header with the actor name and a "Sign out" button that clears storage.
  - Verify: in the browser, sign in as `alice`. A Network-tab request to `/v1/experiments` carries both headers. With `LDE_ENV=prod`, no form appears.
- [x] 3.2 Workspace shell: explorer, tabs, bottom panel
  - Requirements: SPEC-005-R05, SPEC-005-R11
  - Files: `web/src/Workspace.tsx`, `web/src/Explorer.tsx`, `web/src/tabs.ts`, `web/src/BottomPanel.tsx`, `web/src/styles.css`
  - Do:
    - `tabs.ts`: `type Tab = {kind: 'experiment'|'run'|'instrument'|'lineage'|'audit'; id: string; title: string}`. Add a `useTabs()` hook with `open(tab)`, which focuses the tab if `kind:id` is already open, plus `close(key)` and `active`. It syncs the active tab to `location.hash` as `#/<kind>/<id>` and opens it from the hash on load and on `hashchange`.
    - `Workspace.tsx`: the three-region grid from the design sketch, using CSS grid. Tab strip as `role="tablist"`: Left and Right move between tabs, Enter activates, Ctrl+W closes the active tab. Each close button has `aria-label="Close <title>"`. Render placeholder content per kind; 3.3–3.8 replace it.
    - `Explorer.tsx`: `role="tree"` with experiments, which expand to load runs via `listRuns`; instruments; and an "Audit trail" item. Up and Down move, Right and Left expand and collapse, Enter opens. Show a "Load more" item when `next_cursor` is non-null.
    - `BottomPanel.tsx`: tabs "Problems" (shows "No problems") and "History", which shows `listAuditEvents(0, type, id)` for the active tab's entity (`experiment`, `run`, `instrument`, or `lineage_node` for lineage) as time, actor, action and reason.
  - Verify: open an experiment, a run and an instrument from the explorer using the keyboard only. Reopening one focuses its tab. Reloading the page restores the active tab from the hash, and History shows that entity's events.
- [x] 3.3 Experiment view
  - Requirements: SPEC-005-R05
  - Files: `web/src/ExperimentView.tsx`
  - Do: load `getExperiment` and `listProtocolVersions`. Show the title and slug; the current version number and hash; the protocol rendered with `new MarkdownIt({html: false, linkify: false})` into a container via `dangerouslySetInnerHTML` (safe because HTML is disabled); `protocol_meta` as a two-column table, with nested values shown as JSON; and the version list (version, short hash, created by, created at). Add an "Open lineage" button for the experiment's current protocol `node_id`.
  - Verify: an experiment whose protocol contains `<script>alert(1)</script>` shows it as literal text. The version list matches `GET /v1/experiments/{id}/protocol-versions`.
- [x] 3.4 Protocol version diff
  - Requirements: SPEC-005-R15
  - Files: `web/src/diff.ts`, `web/src/ExperimentView.tsx`
  - Do:
    - `diff.ts`: `export function lineDiff(a: string, b: string): {op: ' '|'+'|'-'; line: string}[]`, a standard LCS table over lines, under 50 lines.
    - In the experiment view, add two `<select>`s (from version, to version; defaults are previous and current) and a "Compare" button. Fetch both with `getProtocolVersion` and render the result in a `<pre>`, with added and removed lines styled by token colours **and** a `+` or `-` prefix, so the diff doesn't rely on colour alone.
  - Verify: create v1 and v2 of a protocol differing by one line. Comparing them shows exactly one `-` and one `+` line.
- [x] 3.5 Run view
  - Requirements: SPEC-005-R06
  - Files: `web/src/RunView.tsx`
  - Do: load `getRun`. Show:
    - state;
    - started and finished times;
    - params as a key/value table;
    - the pinned protocol (`v<protocol_version>` and full `protocol_sha256` in monospace);
    - inputs (kind, label);
    - datasets (label, count, full `sha256`, with "not recorded" when null);
    - state history from `listAuditEvents(0, 'run', id)` (time, actor, action, reason).

    Add an "Open lineage" button for `node_id`.
  - Verify: the run from 1.4's verify shows the sample input, the dataset with count 3 and the matching SHA-256, and `run.created` in its history. After a transition through the API and a refresh, the new event appears.
- [x] 3.6 Instrument view
  - Requirements: SPEC-005-R16
  - Files: `web/src/InstrumentView.tsx`
  - Do: show name, kind and `attrs` (key/value table) for the instrument. Find it in the explorer's already-loaded list; if the tab was opened from the hash, page `listInstruments` until it's found. Add an "Open lineage" button for `node_id`.
  - Verify: opening HPLC-01 shows its fields, and the lineage button opens a lineage tab centred on its node.
- [x] 3.7 Lineage explorer
  - Requirements: SPEC-005-R07, SPEC-005-R11
  - Files: `web/src/LineageView.tsx`
  - Do:
    - Register `cytoscape-dagre` once with `cytoscape.use(dagre)`. Load `getLineage(id, direction)`; the default direction is `both`. Map nodes and edges to Cytoscape elements, labelling nodes with `label` and styling by `kind` (colour tokens read with `getComputedStyle`). Lay out with `{name: 'dagre', rankDir: 'LR'}`. Highlight the centre node.
    - Direction control: three radio buttons in a `role="radiogroup"`; changing them refetches.
    - Tapping a node selects it. The side panel shows kind, label, `attrs` (JSON) and created time, plus a "Re-centre here" button that reopens the tab on that node.
    - The side panel also lists every node as a `role="listbox"`. Up and Down select (and highlight in the graph), and Enter re-centres. This is the keyboard path, because the canvas isn't focusable.
    - Destroy the Cytoscape instance on unmount.
  - Verify:
    - Clicking and the keyboard listbox both select and re-centre.
    - Seed a 500-node chain with a curl loop: create node `n0`, then for `i` in 1..499 create node `ni` and an edge `n(i-1) → ni` (`used`). Opening `n499` upstream renders within 2 s (time it in DevTools Performance).
- [x] 3.8 Audit viewer
  - Requirements: SPEC-005-R08
  - Files: `web/src/AuditView.tsx`
  - Do:
    - A table of seq, time, actor, action, entity type, entity id and reason, 100 per page. "Next page" uses `after = last seq`, and "First page" resets.
    - Entity-type and entity-id inputs with "Filter" and "Clear". Send both or neither; disable "Filter" unless both are filled.
    - A "Verify chain" button calls `verifyAudit()` and shows "Chain valid" or "Chain broken at seq N", the engine head and database head (seq and hash), and "Engine and database agree" or "disagree". It's an `aria-live="polite"` region.
  - Verify: page through more than 100 events. Filtering by a run shows only its events. Verify shows valid and agree on the dev tenant.
- [x] 3.9 Command palette
  - Requirements: SPEC-005-R12, SPEC-005-R11
  - Files: `web/src/CommandPalette.tsx`, `web/src/Workspace.tsx`
  - Do:
    - A global `keydown` listener: `(e.ctrlKey || e.metaKey) && e.key === 'k'` → `preventDefault`, open.
    - A modal `role="dialog"` with `aria-modal`, a text input (`role="combobox"`, `aria-controls` the listbox) and a `role="listbox"` of matches.
    - Items:
      - explorer items already loaded (experiments, runs, instruments), each opening its tab;
      - the commands "Open audit trail", "Verify audit chain" (opens the audit tab and runs verify), "Close all tabs" and, in dev only, "Sign out".
    - Filter by case-insensitive substring of the title. Up and Down move, Enter runs and closes, and Esc closes. Focus returns to the element that had it before.
  - Verify: Ctrl+K, type "pla", Enter opens the run "Plate 7". Esc restores focus. It also works on macOS with Cmd+K, or this is checked by code review if no Mac is available.
- [ ] 3.10 Accessibility and bundle check
  - Requirements: SPEC-005-R11
  - Files: `web/src/styles.css` and any view file, only where a fix is needed
  - Do:
    - Keyboard-only walk through every view: sign-in → explorer → each tab kind → bottom panel → palette → audit verify. Fix any trap or unreachable control.
    - Check every token pair used for text against its background with a contrast checker, and fix any below 4.5:1 (3:1 for large text and UI borders).
    - Run `npm run build` and sum `gzip -c` of `dist/assets/*` (`for f in dist/assets/*; do gzip -c "$f"; done | wc -c`). It must be under 1,048,576.
  - Verify: record the contrast pairs and the gzipped total in the task's commit message.

## Phase 4: Playwright harness

- [ ] 4.1 Playwright config, empty suite
  - Requirements: (harness from scope; supports ROADMAP D12)
  - Files: `web/package.json`, `web/package-lock.json`, `web/playwright.config.ts`, `web/e2e/README.md`
  - Do:
    - `npm install --save-dev --save-exact @playwright/test`. Add `"e2e": "playwright test --pass-with-no-tests"`.
    - `playwright.config.ts`: exactly as in `design.md` §3 "Playwright harness".
    - `e2e/README.md`, at most 10 lines:
      - how to run (`make e2e` with the server up, after `npx playwright install chromium`);
      - the `// SPEC-NNN-Rxx` citation convention;
      - that results land in `test-results/results.json`, which SPEC-008 feeds into `make validate`.
    - Make sure `tsconfig.json` excludes `e2e/` and `playwright.config.ts` from the app build, or includes them correctly, so `npm run build` stays clean.
  - Verify: `cd web && npm run e2e` exits 0 with "no tests found" and writes `test-results/results.json`.

## Phase 5: Docs

- [ ] 5.1 README and web/README
  - Requirements: SPEC-005-R01, SPEC-005-R02
  - Files: `README.md`, `web/README.md`
  - Do:
    - `README.md`: in the status table, change the Web UI row from roadmap to done for the workspace shell. Next to the existing commands, add `make ui` (needs Node 22) and say that a binary built without it serves a "UI not built" page.
    - `web/README.md`: replace "Nothing here yet" with the actual layout (one line per `src/` file group), `npm run dev` with the proxy, and the rule that the UI only calls public `/v1` endpoints. Keep the WASM and protocol-editor plans as "later".
    - Keep each change under 20 lines, in the existing tone.
  - Verify: the commands run as written.

## Phase 6: Verify and close out

- [ ] 6.1 `verifier` passes the whole spec
- [ ] 6.2 `uat-planner` writes [uat.md](uat.md)
- [ ] 6.3 Status set to Implemented, folder moved to `specs/implemented/`, `ROADMAP.md` updated
- [ ] 6.4 Summary commit written as the PR description
