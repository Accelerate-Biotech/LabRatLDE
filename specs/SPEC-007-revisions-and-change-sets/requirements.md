# SPEC-007: Revisions, change sets and notes

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-18 |
| **Regulatory refs** | 21 CFR 11.10(e) ("shall not obscure previously recorded information"), 11.10(c) |
| **Depends on** | `SPEC-002`, `SPEC-005`, `SPEC-006` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

Scientists routinely correct records (a mistyped volume), add to them (a late observation) and annotate them (a corrected note). Today LabRat can't edit anything after creation. And if it could, nothing would keep the earlier value, which Part 11 requires. Parsers and agents (SPEC-013, SPEC-014) also need a way to *propose* changes that a human accepts. Git solves both with history, diffs, commits and pull requests. LabRat needs the same mechanics for lab records, without any way to rewrite history.

## 2. Scope

**In scope**
- **Revisions:** an append-only history for each revisable record, written by database triggers so nothing can bypass it.
- **Revisable fields in v1:** lineage node `label` and `attrs`, run `params`, experiment `title`, note `body`.
- **Change sets:** a group of edits with a reason, moving `proposed → committed | rejected | withdrawn`, applied atomically.
- **Direct edits** to unsealed records, recorded as a one-item change set committed in the same request.
- **Notes:** revisioned Markdown attached to a lineage node.
- **History APIs:** log, read one revision, field-level diff, blame.
- **UI:** history/diff panel, notes editor, edit-with-reason, review inbox.

**Out of scope**
- Committing change sets that touch **sealed** records. R10 rejects them until SPEC-008, which defines review, signature invalidation and re-signing.
- Revisions for audit events, signatures, protocol versions or readings (those are immutable).
- Branching or merging lab data.
- Collaborative real-time editing.

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-007-R01 | When a revisable record is inserted or a revisable field changes, the database shall append a revision holding the full new value, the revision number (previous + 1), author, reason, change set id and time, in the same transaction. | yes | 11.10(e) |
| SPEC-007-R02 | If a revisable field changes in a transaction without a committing change set and a non-blank reason, then the database shall reject the statement. | yes | 11.10(e) |
| SPEC-007-R03 | If any role attempts to update, delete or truncate revisions, then the database shall reject the statement with SQLSTATE `LDE03`. | yes | 11.10(c), (e) |
| SPEC-007-R04 | When a change set is committed, the change set API shall apply all of its items, revisions and audit events in one transaction, or none of them. | yes | data integrity |
| SPEC-007-R05 | While a change set is not committed, its items shall not appear in record reads, lineage closures, queries or signature manifests. | yes | data integrity |
| SPEC-007-R06 | If a principal of kind `agent` or `service` commits or rejects a change set, then the database shall reject it with SQLSTATE `LDE05`. | yes | 11.10(g), ROADMAP D6 |
| SPEC-007-R07 | The revision API shall return a record's revisions oldest first, and any single revision's full value. | no | user need |
| SPEC-007-R08 | The revision API shall return a field-level diff (added, removed, changed, with JSON paths) between any two revisions of a record. | no | user need |
| SPEC-007-R09 | The revision API shall return, for each leaf field of a record's latest revision, the revision number, author and time that last changed it. | no | user need |
| SPEC-007-R10 | If a change set touches a sealed record (SPEC-002), then the change set API shall reject its commit with `409` until SPEC-008 defines the correction flow. | yes | 11.10(c) |
| SPEC-007-R11 | When a direct edit is made to an unsealed record, the API shall record it as a single-item change set authored and committed by the editor in the same transaction. | no | uniform history |
| SPEC-007-R12 | While a change set is `proposed`, only its author shall be able to replace its items or withdraw it; `committed`, `rejected` and `withdrawn` shall be terminal. | no | workflow |
| SPEC-007-R13 | When a note is added to a sealed node, the notes API shall accept it as an annotation (`annotation = true`) without breaking the seal; editing a sealed node's existing non-annotation note shall count as touching a sealed record. | yes | 11.10(e) |

## 4. Acceptance examples

Fixture: alice (scientist), bob (reviewer), bot (agent acting for alice). Sample node `S`, label "Plasma 042", attrs `{"volume_ul": 50}`.

**SPEC-007-R01**: alice edits `S` attrs to `{"volume_ul": 45}` with reason "pipetting error". `S` now has revisions 1 (`50`, author alice, reason "created") and 2 (`45`, reason "pipetting error"), and the current row shows `45`. Revision 1 is still readable.

**SPEC-007-R02**: in a transaction that sets no change set, `UPDATE lineage_nodes SET attrs = '{}' WHERE id = S` fails. With a change set but a reason of `"  "`, it also fails.

**SPEC-007-R03**: `UPDATE`, `DELETE` and `TRUNCATE` on `record_revisions`, run as the owner, each fail with `LDE03`.

**SPEC-007-R04**: a change set with items [edit `S` attrs, add edge `S` →used→ `R`, add edge `R` →used→ `S`] is committed. The second edge creates a cycle, so nothing is applied: `S` stays at its previous revision and the audit chain length is unchanged.

**SPEC-007-R05**: bot proposes adding node `N` and editing `S` label to "P-042". Before commit, `GET S` shows "Plasma 042", `N` doesn't exist, and upstream lineage of `R` excludes `N`. After alice commits, both appear.

**SPEC-007-R06**: with bot acting for alice, bot's commit of its own change set fails with `LDE05`.

**SPEC-007-R10**: run `R` is signed (SPEC-002 seal). A change set editing `R` params is created successfully, but its commit returns `409`.

**SPEC-007-R13**: alice adds note "Freezer alarm at 02:10, samples unaffected" to sealed run `R`. It succeeds with `annotation = true` and `R` remains sealed. A change set editing a note written before signing is rejected at commit, as in R10.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should run params stay editable while a run is `running`, or only while `planned`? Recommendation: editable until the first signature, always with a reason. | Rob Bassett | |
| 2 | Should committing someone else's proposed change set (not sealed) need `reviewer`, or is `scientist` enough? Recommendation: `scientist`, since sealed records already need review (SPEC-008). | Rob Bassett | |
