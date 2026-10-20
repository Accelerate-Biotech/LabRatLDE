# SPEC-011: Query and comparison

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | none |
| **Depends on** | `SPEC-005`, `SPEC-009` |

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

## 3. Simplicity budget

- Approach: plain SQL in the handlers. `attrs @? $jsonpath` with a parameterised jsonpath, a GIN index on `attrs`, and `date_bin` bucketing in SQL. Everything runs inside `WithTenant`, so RLS scopes every query. Charts use one small library.
- Not doing: a query language of our own, a search engine (OpenSearch), an OLAP store or materialised views.

## 4. Requirements (EARS)

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

## 6. Data and interfaces

- Indexes (new migration): `lineage_nodes USING gin (attrs jsonb_path_ops)`, `(tenant_id, schema_version_id)`, `(tenant_id, kind, created_at)`, and `pg_trgm` on `label` if the extension is available (open question 2).
- API:
  - `GET /v1/lineage/nodes?kind=&schema=&label=&created_after=&created_before=&where=<jsonpath>&cursor=&limit=`
  - `GET /v1/datasets/{id}/channels`
  - `GET /v1/datasets/{id}/readings?channel=&from=&to=&bucket=&cursor=&limit=`
  - `POST /v1/compare/series {dataset_ids, channel, bucket}`
  - `POST /v1/compare/attrs {node_ids}`

## 7. UI notes

- Search lives in the explorer's search tab. Choosing a schema adds typed filter fields; results open as tabs or can be multi-selected for "Compare".
- Dataset chart: one line per channel, a min/max band when bucketed, a unit on each axis, and drag to zoom.
- Comparison view: overlaid series with a legend per dataset (run label, instrument), and the attribute table below.

## 8. Non-functional

- With 1,000,000 nodes in a tenant, a kind + jsonpath search shall return its first page within 500 ms at p95.
- Bucketed readings for a 1,000,000-point dataset into ≤ 2,000 buckets shall return within 2 s.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Is full-text search over notes and protocol text needed in v1? Recommendation: later; PostgreSQL `tsvector` when it is. | Rob Bassett | |
| 2 | Can deployments rely on the `pg_trgm` extension (it ships with PostgreSQL contrib)? If not, label search uses `ILIKE` without an index. | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Filters use PostgreSQL SQL/JSON path, passed as a bound parameter | Expressive, indexed by GIN, no custom language | A home-grown filter DSL |
| 2026-10-20 | Frontend dependency `uplot` for charts | ~50 KB, handles 10⁵–10⁶ points | Chart.js, Plotly (heavier) |
