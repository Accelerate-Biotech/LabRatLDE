# End-to-end tests

Run with the server up (`make up`, then `make ui` and `go run ./cmd/server`), after `npx playwright install chromium` (once).

- Run: `make e2e` (set `LABRAT_URL` to target another server; default `http://localhost:8080`).
- Every test cites its requirement on the line above `test(`: `// SPEC-NNN-Rxx`.
- Results are written to `web/test-results/results.json`; SPEC-008 feeds this into `make validate`.
