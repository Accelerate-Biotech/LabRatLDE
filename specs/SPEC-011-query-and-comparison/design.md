# SPEC-011 design: Query and comparison

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: plain SQL in the handlers. `attrs @? $jsonpath` with a parameterised jsonpath, a GIN index on `attrs`, and `date_bin` bucketing in SQL. Everything runs inside `WithTenant`, so RLS scopes every query. Charts use one small library.
- Not doing: a query language of our own, a search engine (OpenSearch), an OLAP store or materialised views.

## 2. Data and interfaces

- Indexes (new migration): `lineage_nodes USING gin (attrs jsonb_path_ops)`, `(tenant_id, schema_version_id)`, `(tenant_id, kind, created_at)`, and `pg_trgm` on `label` if the extension is available (open question 2).
- API:
  - `GET /v1/lineage/nodes?kind=&schema=&label=&created_after=&created_before=&where=<jsonpath>&cursor=&limit=`
  - `GET /v1/datasets/{id}/channels`
  - `GET /v1/datasets/{id}/readings?channel=&from=&to=&bucket=&cursor=&limit=`
  - `POST /v1/compare/series {dataset_ids, channel, bucket}`
  - `POST /v1/compare/attrs {node_ids}`

## 3. UI

- Search lives in the explorer's search tab. Choosing a schema adds typed filter fields; results open as tabs or can be multi-selected for "Compare".
- Dataset chart: one line per channel, a min/max band when bucketed, a unit on each axis, and drag to zoom.
- Comparison view: overlaid series with a legend per dataset (run label, instrument), and the attribute table below.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Filters use PostgreSQL SQL/JSON path, passed as a bound parameter | Expressive, indexed by GIN, no custom language | A home-grown filter DSL |
| 2026-10-20 | Frontend dependency `uplot` for charts | ~50 KB, handles 10⁵–10⁶ points | Chart.js, Plotly (heavier) |
