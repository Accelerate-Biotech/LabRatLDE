# SPEC-010: Schema governance namespaces

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(k), 11.50, 11.70; EU Annex 11 §10 (change management) |
| **Depends on** | `SPEC-006`, `SPEC-007`, `SPEC-008`, `SPEC-009` |
| **Supersedes** | `SPEC-009`'s rule that any scientist may publish |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

R&D teams change data definitions weekly. GMP manufacturing must change them only under formal change control. Today a schema change has no owner, nothing stops a breaking change from silently invalidating downstream pipelines, and nobody signs off on changes to controlled master data. LabRat needs one registry whose strictness depends on where a schema lives, with a clearly responsible person: the **Schema Steward**.

## 2. Scope

**In scope**
- **Namespaces**, each with a governance level (`sandbox`, `contract`, `controlled`) and one or more Stewards (`schema_steward` role scoped to the namespace, SPEC-006).
- Per-level publishing rules, and a **compatibility linter** for each level.
- **Change requests:** publishing in `contract` or `controlled` goes through a SPEC-007 change set with op `publish_schema_version`.
- For `controlled`: Steward and QA e-signatures on the change request (SPEC-008 mechanism, `entity_type = change_set`), plus an attached **impact analysis**.
- **Promotion:** copying a schema up a level, recording where it came from.
- **Ontology:** CURIE fields, and a tenant prefix registry.
- **Optional GitOps:** a service principal (CI) may submit change requests but never commit them.
- UI: change-request review with a schema diff, lint results and impact list.

**Out of scope**
- An ontology term browser or lookup service (CURIEs are checked for a known prefix only).
- Automatic data migration between major versions.
- Lowering a namespace's level.

## 3. Requirements (EARS)

The governance levels:

| Level | Publishes | Compatibility | Approval |
|---|---|---|---|
| `sandbox` | author | may only extend published parents with optional fields | none |
| `contract` | Steward commits the change request | additive-only within a major version | Steward |
| `controlled` | Steward commits after signatures | breaking changes blocked within a major version; a new major version needs impact analysis | Steward `approval` + QA `approval` signatures |

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-010-R01 | If a namespace's level is changed to a lower level, then the database shall reject the change. | yes | change control |
| SPEC-010-R02 | If the last active Steward of a `contract` or `controlled` namespace would be revoked, then the database shall reject the revocation. | no | accountability |
| SPEC-010-R03 | If a sandbox schema that extends a published schema adds a required property, redefines a parent property, or sets `additionalProperties: false`, then the compatibility linter shall reject it. | yes | Sandbox Rule |
| SPEC-010-R04 | If a new minor version in a `contract` or `controlled` namespace removes a property, changes a type, adds a required property, narrows an enum, range, length or pattern, or tightens `additionalProperties` compared with the previous published minor of the same major, then the compatibility linter shall reject it. | yes | data contract |
| SPEC-010-R05 | If a schema version in a `contract` or `controlled` namespace is published other than by committing a change request whose committer is a Steward of that namespace, then the database shall reject it with SQLSTATE `LDE05`. | yes | 11.10(k) |
| SPEC-010-R06 | If a change request publishing into a `controlled` namespace is committed without valid `approval` signatures from a Steward of that namespace and from a `qa` principal, then the database shall reject the commit with SQLSTATE `LDE07`. | yes | 11.10(k), 11.50 |
| SPEC-010-R07 | When a change request targets a `controlled` namespace, the registry shall attach an impact analysis listing protocol versions, nodes, runs and published schemas that reference earlier versions of the schema, and the signature manifest shall include its SHA-256. | no | Annex 11 §10 |
| SPEC-010-R08 | When a schema is promoted, the registry shall create it in the target namespace as a new schema whose version records `promoted_from`, subject to the target level's rules. | no | traceability |
| SPEC-010-R09 | If a schema in a `controlled` namespace lacks a root `x-lde-ontology` CURIE, or uses a CURIE whose prefix isn't in the tenant prefix registry, then the compatibility linter shall reject it. | no | ontology mapping |
| SPEC-010-R10 | If a principal of kind `service` submits a change request, then the registry shall store it as `proposed` and the principal shall never commit it (SPEC-007-R06). | no | GitOps |
| SPEC-010-R11 | Where a `controlled` namespace sets `required_for_kind`, the database shall reject nodes of that kind created without a schema from that namespace. | no | master data |

## 4. Acceptance examples

Fixture: namespaces `~alice` (sandbox), `mol-bio` (contract, Steward bob), `gmp-materials` (controlled, Steward erin, QA quinn). Schema `mol-bio/sample@1.0` = SPEC-009's fixture.

**SPEC-010-R01**: setting `gmp-materials` to `contract` fails. Setting `mol-bio` to `controlled` succeeds.

**SPEC-010-R03**: in `~alice`, `{"allOf":[{"$ref":"lde://schemas/mol-bio/sample/1.0"}],"properties":{"hemolysis_score":{"type":"integer"}}}` publishes. Adding `"required":["hemolysis_score"]`, redefining `volume_ul` as a string, or adding `"additionalProperties": false` is each rejected.

**SPEC-010-R04**: `mol-bio/sample@1.1` that adds optional `collected_at` passes. Versions that drop `matrix`, make `matrix` required, change `minimum` to 10, or remove `"serum"` from the enum are each rejected. The same change as `2.0` passes the linter.

**SPEC-010-R05**: alice (scientist) publishing `mol-bio/sample@1.1` directly fails with `LDE05`. Alice's change request committed by bob succeeds.

**SPEC-010-R06**: a change request for `gmp-materials/cell-line@1.1` signed by erin only → commit fails with `LDE07`. After quinn signs `approval` as well, erin's commit succeeds.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | In `contract` namespaces, may a Steward approve their own change request? Recommendation: yes for `contract` (R&D speed), no for `controlled` (QA signs anyway). | Rob Bassett | |
| 2 | Which ontology prefixes should ship by default (e.g. CLO, ChEBI, OBI, UO, NCBITaxon, EFO)? | Rob Bassett | |
| 3 | Can a new major version in `controlled` be published at all, or must it be a new schema name? Recommendation: allowed with impact analysis and both signatures. | Rob Bassett | |
