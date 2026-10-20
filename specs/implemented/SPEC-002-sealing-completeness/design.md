# SPEC-002 design: Sealing completeness

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one SQL function, `lineage_node_sealed(node_id)`, used by `BEFORE` triggers on `lineage_edges`, `lineage_nodes` and `instrument_readings`, all added in one new migration. Grant changes go in the same migration.
- Not doing: a generic "lock" table, application-level checks only, or a seal flag column that would have to be kept in sync.

## 2. Data and interfaces

- New migration: `lineage_node_sealed(uuid) RETURNS boolean` (`STABLE`), triggers `lineage_edges_sealed`, `lineage_nodes_sealed` and `instrument_readings_sealed`, plus `REVOKE UPDATE ON experiments, lineage_nodes, instruments FROM lde_app`.
- R07 mechanism (implementer's choice, documented in the migration): e.g. compare the dataset node row's `xmin` with `pg_current_xact_id()`, or keep a transaction-local set of datasets created in this transaction.
- New SQLSTATE `LDE04` = "sealed record". `storage` maps it to a conflict.
- No new endpoints and no new audit actions.

## 3. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Sealed set = run node + datasets it generated; downstream use stays allowed | Sealing must protect what was signed without stopping science from building on it | Sealing the full downstream closure (blocks all reuse) |
| 2026-10-18 | Revoke unused `UPDATE` grants instead of auditing them by trigger | No endpoint needs them; SPEC-007 re-grants column-level `UPDATE` with revision triggers | Audit triggers on every table now |
| 2026-10-20 | `aborted` runs seal immediately (R01) | Answer to open question 1 | Leaving aborted runs open |
| 2026-10-20 | R07 compares the dataset node's `xmin` with `pg_current_xact_id()` | No extra state to keep in sync. Fails closed if the node was created in a savepoint, which ingest never does | Transaction-local set of created datasets |
| 2026-10-20 | `instrument_readings_sealed` is an `AFTER INSERT ... FOR EACH STATEMENT` trigger with a transition table, not a `BEFORE` row trigger | One check per dataset per 5000-row ingest batch instead of one per reading; the rejected statement still rolls back whole | Per-row `BEFORE` trigger |
| 2026-10-20 | `lineage_edges_sealed` takes `FOR SHARE` on the runs whose seal an edge depends on | Without it, an edge and a concurrent sign could both commit under READ COMMITTED, leaving a signed run with a new input or output | Relying on the API's `FOR UPDATE` in `transitionRun` (not a database invariant) |
