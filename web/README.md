# web/ — LabRat frontend (planned)

The UI is a client of the public API, never a privileged path around it.

Planned shape:

- **React + TypeScript** shell, built to static assets and embedded into the Go
  binary with `embed.FS`, so deployment stays one file.
- **WebAssembly modules** (Go or Rust compiled to WASM) for the heavy lifting
  in the browser: chromatogram rendering, Cq curve fitting, plate-map
  operations on millions of points, without round-tripping to the server.
- **Lineage explorer**: an interactive DAG view backed by
  `GET /v1/lineage/{id}?direction=both`.
- **Protocol editor**: Markdown + YAML frontmatter with live schema validation.

Nothing here yet. Contributions welcome; open an issue to discuss before
starting.
