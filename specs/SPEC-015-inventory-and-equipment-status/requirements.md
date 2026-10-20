# SPEC-015: Inventory and equipment status

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(e) (status changes are revisions) |
| **Depends on** | `SPEC-007`, `SPEC-009`, `SPEC-012` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

Pre-flight checks (SPEC-016) need to know whether a reagent lot has expired or been quarantined, and whether an instrument is in calibration. Today reagent lots are bare lineage nodes and instruments carry free-form attrs, so neither question has a reliable answer. Lab managers also need a simple view of what's expiring and which equipment is due for calibration.

## 2. Scope

**In scope**
- Starter schemas in the system namespace `lde` (controlled level):
  - `lde/reagent-lot@1.0`: catalog number, lot number, supplier, received date, expiry date, quantity and unit, storage condition, status;
  - `lde/instrument-status@1.0`: asset ID, status, last calibrated, calibration due, maintenance due, location.
- A status vocabulary: lots are `quarantined`, `released`, `rejected` or `consumed`; instruments are `in_service`, `out_of_service` or `quarantined`.
- Status and date changes are SPEC-007 revisions with a reason, and calibration certificates can be linked as raw files (SPEC-012).
- Releasing or rejecting a lot requires `qa`.
- A tenant time zone setting, used to evaluate dates.
- UI: inventory table and equipment status board.

**Out of scope**
- Automatic quantity decrement when a run uses a lot (open question 1).
- Barcodes and labels, ordering, freezer location maps.
- Calibration scheduling and notifications.
- An inventory system of record; LabRat may mirror one later.

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-015-R01 | When the migration runs, the database shall contain published versions of `lde/reagent-lot@1.0` and `lde/instrument-status@1.0` in a `controlled` namespace `lde`. | no | product content |
| SPEC-015-R02 | If a reagent lot's `status` is changed to `released` or `rejected` by a principal without `qa`, then the database shall reject the change with SQLSTATE `LDE05`. | yes | GMP material release |
| SPEC-015-R03 | When a reagent lot is created without a status, the inventory API shall set it to `quarantined`. | no | GMP practice |
| SPEC-015-R04 | When a lot's or instrument's status or dates change, the change shall be recorded as a SPEC-007 revision with a non-blank reason. | no | 11.10(e) |
| SPEC-015-R05 | When a calibration is recorded, the API shall accept an optional raw file (certificate) and link the instrument node `wasDerivedFrom` it. | no | traceability |
| SPEC-015-R06 | The tenant shall have a time zone (IANA name, default `UTC`) that every date comparison in SPEC-016 uses. | no | correctness |
| SPEC-015-R07 | The inventory UI shall list lots with filters for status and "expires within N days", highlighting expired and expiring lots. | no | user need |
| SPEC-015-R08 | The equipment UI shall show each instrument's status and calibration due date, flagging overdue (red) and due within 14 days (amber). | no | user need |

## 4. Acceptance examples

**SPEC-015-R02**: alice (scientist) changes lot `L1` status from `quarantined` to `released` with reason "CoA ok" → `LDE05`. Quinn (qa) makes the same change → it succeeds, and the revision shows quinn as author. Alice changing `quantity` succeeds.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should using a lot in a run record quantity consumed (edge attributes or a usage record)? Recommendation: not in v1. | Rob Bassett | |
| 2 | Is a lot usable on its expiry date (expires end of day) or not? Recommendation: usable through the end of the expiry date in the tenant time zone. | Rob Bassett | |
