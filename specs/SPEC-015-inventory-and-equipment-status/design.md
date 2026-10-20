# SPEC-015 design: Inventory and equipment status

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: no new tables. Lots are `reagent` nodes and instruments are `instrument` nodes carrying these schemas. The `lde` schemas ship in a migration. The QA gate is a trigger checking `lde_has_role('qa')` when a lot's `status` changes to `released` or `rejected`. Tenant time zone is one column on `tenants`.
- Not doing: separate inventory tables, a stock ledger, or a calibration module.

## 2. Data and interfaces

- `tenants.time_zone text NOT NULL DEFAULT 'UTC'`, which an admin can change (audited).
- Schemas: `lde/reagent-lot@1.0` (`required: catalog_no, lot_no, supplier, expiry_date, status`), `lde/instrument-status@1.0` (`required: status, calibration_due`). Dates use `format: date`.
- API: uses existing node, revision and raw-file endpoints. Convenience endpoints `GET /v1/inventory/lots?status=&expires_within_days=` and `GET /v1/equipment`.

## 3. UI

- Inventory and Equipment are explorer sections. Rows open the node in the editor, with schema forms from SPEC-009. Status changes prompt for a reason.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Inventory and equipment are schema-backed nodes, not new tables | Reuses lineage, revisions, forms and governance | Dedicated inventory tables |
