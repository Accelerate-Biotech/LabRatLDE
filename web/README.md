# web/ — LabRat frontend

The UI is a client of the public API, never a privileged path around it. Every
call goes to a public `/v1` endpoint that a curl user could also call.

## Layout

`src/` holds the React + TypeScript app:

- `api.ts`: typed client for `/v1`. The only file that calls `fetch`.
- `App.tsx`, `SignIn.tsx`: app shell and the dev sign-in form.
- `Workspace.tsx`, `Explorer.tsx`, `tabs.ts`, `BottomPanel.tsx`, `CommandPalette.tsx`: the workspace, navigation, tabs, bottom panel and command palette.
- `ExperimentView.tsx`, `RunView.tsx`, `InstrumentView.tsx`, `LineageView.tsx`, `AuditView.tsx`: one view per object kind.
- `diff.ts`: line diff for protocol versions.
- `styles.css`: the only stylesheet.

`e2e/` holds the Playwright harness (empty for now).

## Build

- `make ui` runs `npm ci && npm run build` in this directory. Output goes to `web/dist`.
- `web/web.go` embeds `dist` into the Go binary, so the server serves the UI from the same origin as the API.
- No Node is needed to build the Go binary. Without `make ui`, `/` shows a "UI not built" page.

## Develop

Run the Go server (`go run ./cmd/server`) and then `npm run dev` here. Vite
serves the UI on its own port and proxies `/v1` to `:8080`.

## Test

`make e2e` runs the Playwright suite. It passes with no tests until the
browser tests are written (SPEC-005 phase 4).

## Later

Planned, not built:

- **WebAssembly modules** (Go or Rust compiled to WASM) for chromatogram
  rendering, Cq curve fitting and plate-map operations in the browser.
- **Protocol editor**: Markdown + YAML frontmatter with live schema validation.

Contributions welcome; open an issue to discuss before starting.
