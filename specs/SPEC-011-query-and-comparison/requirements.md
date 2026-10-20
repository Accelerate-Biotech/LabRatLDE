# SPEC-011: Query and comparison

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | none |
| **Depends on** | `SPEC-005`, `SPEC-009` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

The design promises "native cross-experiment comparison without manual CSV exports". Today the only read paths are fetching one record and walking lineage. A scientist can't ask "show every plasma sample with volume under 40 µL", look at a chromatogram, or overlay three runs' curves. Instrument readings can reach 10⁶ points per dataset, so charts need server-side downsampling.

## 2. Scope

**In scope**
- Node search by kind, schema, label text, creation time and JSONB path predicates over `attrs`, with cursor pagination.
- A dataset channel summary, and reading pages with optional time-bucket aggregation.
- Comparison: aligned series for up to 20 datasets on one channel, and a side-by-side attribute table for up to 50 nodes.
- UI: search panel, dataset chart, comparison view.

**Out of scope**
- Saved queries, dashboards and alerts.
- Free-text search inside notes and protocols (open question 1).
- File exports (SPEC-017). The UI may copy a visible table to the clipboard only.
- Analysis and statistics beyond min, max, mean and count per bucket.

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-011-R01 | When nodes are searched, the query API shall filter by any combination of kind, schema (`ns/name` or `ns/name@major`), label substring (case-insensitive), created-at range and one SQL/JSON path predicate over `attrs`, returning at most 500 per page with a cursor. | no | user need |
| SPEC-011-R02 | If a jsonpath predicate is syntactically invalid, then the query API shall respond `400` naming the error, and shall never interpolate user text into SQL. | no | security |
| SPEC-011-R03 | When a dataset's channels are requested, the query API shall return each channel's unit, count, min, max, first and last timestamp. | no | user need |
| SPEC-011-R04 | When readings are requested without a bucket, the query API shall return them in timestamp order, at most 50,000 per page with a cursor. | no | user need |
| SPEC-011-R05 | When readings are requested with `bucket=<duration>`, the query API shall return, per channel and bucket, the count, min, max and mean, computed over every reading in the bucket. | no | user need |
| SPEC-011-R06 | When up to 20 datasets are compared on one channel, the query API shall return each series with timestamps relative to that dataset's first reading, using the same bucket for all. | no | user need |
| SPEC-011-R07 | When up to 50 nodes are compared, the query API shall return a table with one row per leaf attribute path and one column per node, marking values that differ. | no | user need |
| SPEC-011-R08 | The UI shall offer search with filter fields generated from the selected schema, a dataset chart with zoom (re-querying with a finer bucket), and a comparison view reachable from search results and the lineage explorer. | no | user need |

No `Test: yes` requirements. These are read paths; tenant isolation is already tested under SPEC-001-R08, and SPEC-007-R05 covers keeping uncommitted data hidden.

## 4. Non-functional

- With 1,000,000 nodes in a tenant, a kind + jsonpath search shall return its first page within 500 ms at p95.
- Bucketed readings for a 1,000,000-point dataset into ≤ 2,000 buckets shall return within 2 s.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Is full-text search over notes and protocol text needed in v1? Recommendation: later; PostgreSQL `tsvector` when it is. | Rob Bassett | |
| 2 | Can deployments rely on the `pg_trgm` extension (it ships with PostgreSQL contrib)? If not, label search uses `ILIKE` without an index. | Rob Bassett | |
