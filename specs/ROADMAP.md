# LabRat roadmap

| | |
|---|---|
| **Owner** | Rob Bassett |
| **Updated** | 2026-10-30 |
| **Baseline** | [`SPEC-001`](implemented/SPEC-001-core-integrity/requirements.md) (Implemented) at commit `76a7ff0` |

This is the plan for getting from the scaffold to a regulated pilot and beyond. Each spec is a folder holding `requirements.md`, `design.md` and `tasks.md` (template in `TEMPLATE/`); once implemented and verified it moves to `implemented/` with a `uat.md` user acceptance test plan. Every brief in §5 now has a **Draft** spec. Each spec lists its open questions; resolve them (the `spec-writer` agent can help: `claude --agent spec-writer`), then mark the spec Approved before any work starts. The briefs in §5 are kept as the summary. Where a spec refines a brief's dependencies, the spec wins.

| Spec | File | Status |
|---|---|---|
| SPEC-002 | [Sealing completeness](implemented/SPEC-002-sealing-completeness/requirements.md) | Implemented |
| SPEC-003 | [Immutable protocol versions](implemented/SPEC-003-protocol-versions/requirements.md) | Implemented |
| SPEC-004 | [Traceability harness](implemented/SPEC-004-traceability-harness/requirements.md) | Implemented |
| SPEC-005 | [UI stack and workspace shell](implemented/SPEC-005-ui-workspace-shell/requirements.md) | Implemented |
| SPEC-006 | [Identity and roles](implemented/SPEC-006-identity-and-roles/requirements.md) | Implemented |
| SPEC-007 | [Revisions, change sets and notes](SPEC-007-revisions-and-change-sets/requirements.md) | Approved |
| SPEC-008 | [Electronic signatures v2](SPEC-008-electronic-signatures/requirements.md) | Draft |
| SPEC-009 | [Schema registry and schema-driven forms](SPEC-009-schema-registry-and-forms/requirements.md) | Draft |
| SPEC-010 | [Schema governance namespaces](SPEC-010-schema-governance/requirements.md) | Draft |
| SPEC-011 | [Query and comparison](SPEC-011-query-and-comparison/requirements.md) | Draft |
| SPEC-012 | [Raw file custody](SPEC-012-raw-file-custody/requirements.md) | Draft |
| SPEC-013 | [Parser workers and ingest review](SPEC-013-parser-workers-and-ingest-review/requirements.md) | Draft |
| SPEC-014 | [MCP server](SPEC-014-mcp-server/requirements.md) | Draft |
| SPEC-015 | [Inventory and equipment status](SPEC-015-inventory-and-equipment-status/requirements.md) | Draft |
| SPEC-016 | [Pre-flight checks v1](SPEC-016-preflight-checks/requirements.md) | Draft |
| SPEC-017 | [Open exports](SPEC-017-open-exports/requirements.md) | Draft |
| SPEC-018 | [Audit chain anchoring](SPEC-018-chain-anchoring/requirements.md) | Draft |
| SPEC-019 | [CLI (optional)](SPEC-019-cli/requirements.md) | Draft |
| SPEC-020 | Validation documentation | Brief only (§5, Phase 1) |
| SPEC-021 | [Reference identity broker and IdP recipes](SPEC-021-identity-broker/requirements.md) | Draft |

**SQLSTATE registry** (custom codes raised by triggers): `LDE01` lineage cycle (SPEC-001), `LDE02` forged or non-extending audit event (SPEC-001), `LDE03` immutable/append-only record (SPEC-001, reused), `LDE04` sealed record (SPEC-002), `LDE05` role denied (SPEC-006), `LDE06` invalid schema reference (SPEC-009), `LDE07` signature rule violation (SPEC-008).

## 1. Where the build stands against the design

| Design element | State | Notes |
|---|---|---|
| Go single binary, API-first | Built | `cmd/server`, distroless image |
| Postgres + RLS + JSONB | Built | `0003_rls.sql`, FORCE'd and fail-closed |
| Append-only hash-chained audit | Built | Digest enforced by trigger; three verifiers agree |
| Global lineage DAG | Built | Acyclicity trigger, closure query |
| Run lifecycle and signatures | Partial | A signature is a state plus a free-text reason, not a signature record |
| Instrument ingest | Partial | NDJSON numeric readings only; no raw vendor files or parsers |
| Protocols as code | Built | Append-only, gapless versions with a DB-computed SHA-256; each run pins its version immutably (SPEC-003) |
| Identity | Dev headers only | `LDE_ENV=prod` refuses to serve |
| Web UI | Not started | Core to the product (decision D7) |
| Schemas and governance tiers | Not started | `attrs` and `params` are unvalidated |
| Revisions / Git-like workflow | Not started | |
| MCP / agents, pre-flight checks | Not started | |
| Open exports, chain anchoring | Not started | |
| Validation as code | Partial | `make validate` traceability report and `labrat iq` (SPEC-004); no risk assessment, protocols or expected-vs-actual evidence yet (SPEC-020) |

### Integrity gaps in the current build

These are correctness issues in what exists, not missing features. SPEC-002 and SPEC-003 close them.

1. **Sealing stops at the run row.** After a run is signed, the database still accepts new `used` edges into its node, new datasets `wasGeneratedBy` it, and `UPDATE` of `lineage_nodes.attrs`, because `lde_app` holds UPDATE in `0003_rls.sql`.
2. **An unaudited mutation path exists in the DB.** `lde_app` can `UPDATE` `experiments`, `lineage_nodes` and `instruments`. No endpoint does so, but no trigger audits it either.
3. ~~**Runs don't pin their protocol.**~~ Closed by SPEC-003: a run pins an immutable protocol version and its hash.
4. **A signature isn't a record.** §11.50/§11.70 need, for each signature: the signer's printed name, the time, a controlled meaning, and a binding to the content signed. Real sign-off is also multi-party (author → reviewer → QA).

## 2. Design decisions and corrections

| # | Decision | Why |
|---|---|---|
| D1 | The audit ledger is a **linear per-tenant hash chain**, not "Merkle-style". Later proofs and anchoring come from signed checkpoints (SPEC-018), never from changing digest v1. | Accuracy. Digest v1 is frozen. |
| D2 | Performance claims are stated as measured NFRs, e.g. readings per request and audited mutations per second per tenant, never as "no degradation". | Each tenant's writes serialise on the chain head and the lineage lock. That's cheap but not free. |
| D3 | **JSON Schema 2020-12** is the stored schema format. LinkML is optional, compiled upstream. Conformance is checked in Go; the DB enforces that a claimed schema version exists, is published and is immutable. | Mature Go tooling. Honest about which invariants the DB holds. |
| D4 | **The three governance tiers are levels on one schema registry**, with a named Schema Steward per namespace (§3). | R&D needs speed and GMP manufacturing needs change control. One mechanism, configured per namespace, covers both. |
| D5 | **"Git for lab records" means append-only revisions, change sets and review**: no rebase, no force-push, no branching of lab data (§4). | Corrections, appends and corrected notes are routine. Part 11 forbids obscuring prior values. |
| D6 | **Agents are clients, never part of the engine.** The binary stays LLM-free. Agents are distinct principals acting `on_behalf_of` a human; they can propose but never commit or sign. | Determinism, validation scope, air-gap, and §11.100. |
| D7 | **The web UI is core.** The primary users are bench scientists. Every feature spec from Phase 1 on has a mandatory UI section and ships with its screens. "The API is the product" survives as a discipline: the UI gets no private endpoint. | An LDE that scientists can't use is just an API. |
| D8 | UI assets are **embedded in the binary** (`go:embed`). | Keeps the single-binary, air-gapped deployment. |
| D9 | **Raw vendor files are kept**, content-addressed by SHA-256, and parsed into open formats. Parsers run **out of process** and submit change sets. | Part 11 original records. "No proprietary blobs" means never *our* lock-in, not discarding originals. Parsers churn and shouldn't sit in the validated core. |
| D10 | Machine-produced matches (sample fuzzy-matching, parsed data) **never auto-commit**. | Human accountability for the record. |
| D11 | Pre-flight checks start **deterministic**: schema, calibration and lot expiry. No open-ended "chemistry rules". | Scope you can actually validate. |
| D12 | **The OQ is the `Test: yes` suite** (API + DB), plus Playwright for critical UI flows once the UI exists. `make validate` produces the traceability matrix and a stamped report. | The testing policy already is validation as code. |
| D13 | The CLI is optional automation tooling, not the primary interface. | Follows from D7. |

## 3. Schema governance model

One schema registry. Every schema lives in a **namespace**. Each namespace has a **governance level** and a **Schema Steward**, a named person assigned per namespace whose assignment is an audited role grant (SPEC-006).

| Level | Typical use | Publishes | Compatibility rule | Approval |
|---|---|---|---|---|
| **Sandbox** (Tier 1) | Personal or project R&D exploration | Author | May `extend` a published schema with optional fields; never alters it (the Sandbox Rule) | None |
| **Contract** (Tier 2) | Team and lab data contracts, R&D production | Schema Steward | Additive-only within a major version; a breaking change means a new major version plus a migration note | Steward approves the change request |
| **Controlled** (Tier 3) | GMP manufacturing; master entities (compounds, cell lines); ontology-mapped | Schema Steward + QA | Breaking changes blocked. Every change carries a change-control record with impact analysis (dependent protocols, runs, consumers) | Steward + QA **e-signatures** |

- Published versions are immutable at every level.
- Promotion (Sandbox → Contract → Controlled) is a change request. Change requests are SPEC-007 change sets, so schemas, corrections and ingest share one review flow and one review UI.
- Schemas may be authored in git, with CI submitting change requests (optional GitOps). Approval always happens in LabRat, where it is signed and audited.
- Ontology mapping starts as CURIE-typed fields (`CLO:0000031`), required only in Controlled namespaces.

## 4. Revisions and change sets ("Git for lab records")

| Git idea | LabRat equivalent |
|---|---|
| Commit history | An append-only **revision** history for each mutable record (run fields, node attrs, notes): parent, author, time, reason |
| `log` / `diff` / `blame` | API and UI over revisions |
| Commit / pull request | A **change set**: one or more edits with a reason, moving `proposed → committed / rejected` and committed atomically |
| Review | Changes to unsealed records commit directly. Corrections and appends to **sealed** records need a reviewed change set |
| Amending a signed commit | Committing a correction **invalidates the affected signatures** and sends the record back for re-signing; the old revision and signatures stay visible |
| Branches | Only for text artifacts (protocols, schemas) and Sandbox namespaces. Lab data isn't branched |
| Rebase / force-push | Never |

Notes are first-class, revisioned Markdown attached to runs and nodes. Policy decides whether an append needs re-signing, e.g. a note doesn't but new data does.

## 5. Spec briefs

`Test` lists the rules expected to be marked `Test: yes`. Everything else is checked by review.

### Phase 0: Close integrity gaps and lay the UI foundation

| ID | Title | Scope | Test | Regulatory | Deps |
|---|---|---|---|---|---|
| SPEC-002 | Sealing completeness | Once a run is signed, the DB rejects new edges touching its node, readings for its datasets, and attr updates to those nodes. Revoke unused `UPDATE` grants, or audit them with a trigger | Each sealing rule, against the DB | 11.10(c), (e) | 001 |
| SPEC-003 | Immutable protocol versions | Append-only `protocol_versions` with SHA-256; a version is a `protocol` lineage node (prov:Plan); runs pin `protocol_version_id` and the hash; an edit creates a new version | Pinned hash matches stored text; versions are immutable | 11.10(e) | 001 |
| SPEC-004 | Traceability harness | `make validate` runs the suite with `-json`, maps tests to requirement IDs in `specs/`, and emits a traceability matrix in a report stamped with git commit, migrations and time, with the report hash recorded. `labrat iq` runs the install checks. Evidence only, not a validation package (see SPEC-020) | Any `Test: yes` requirement without a passing test is flagged | 11.10(a) | none |
| SPEC-005 | UI stack and workspace shell | Stack decision (React + TypeScript recommended); assets embedded with `go:embed`; IDE-style shell (explorer, editor pane, problems panel, history panel); read-only views: experiments, run, lineage explorer, audit viewer; Playwright harness wired into SPEC-004 | None (UI plumbing) | none | none |

### Phase 1: Identity, roles, revisions and signatures (blocks any regulated use)

| ID | Title | Scope | Test | Regulatory | Deps |
|---|---|---|---|---|---|
| SPEC-006 | Identity and roles | OIDC bearer tokens; tenant from claims; principal kinds `human` / `service` / `agent` (`on_behalf_of`); roles `scientist`, `reviewer`, `schema_steward` (per namespace), `qa`, `admin`; role grants audited; role-gated actions checked in the DB; dev headers only in `LDE_ENV=dev`. UI: login, my roles | No tenant access without auth; role gates; prod rejects dev headers | 11.10(d), (g), 11.300 | 005 |
| SPEC-007 | Revisions, change sets and notes | §4 above: revision tables, the change-set lifecycle, `log`/`diff`/`blame`, notes, reviewed changes to sealed records. UI: history/diff panel, notes editor, review inbox | Revisions never altered or removed (DB); a correction keeps the prior value; commits are atomic; sealed records change only through a reviewed change set | 11.10(e) | 002, 006 |
| SPEC-008 | Electronic signatures v2 | `signatures` record (signer, printed name, controlled meaning, time, manifest digest of the signed revision: run + datasets + notes); step-up re-auth; ordered multi-signature policy; a correction invalidates signatures; agents and services can't sign (DB). UI: sign dialog with re-auth, sign-off inbox | Manifest binding; invalidation on correction; agents can't sign; re-auth freshness; sealing | 11.50, 11.70, 11.100, 11.200 | 002, 006, 007 |
| SPEC-021 | Reference identity broker and IdP recipes | LabRat stays OIDC-only. A hardened Keycloak realm template (one per tenant) brokers a site's SAML or OIDC IdP and issues LabRat-shaped tokens; it allows no local passwords and no email linking, and passes re-auth upstream. `labrat idp-check` diagnoses tokens through `identify`'s own path. Recipes for Entra and Okta direct, SAML and Entra via the broker, each verified on a real tenant. The org operates the broker | Broker token shape; bad SAML refused; no local login or email linking; `prompt=login` forces upstream re-auth (Playwright, against Keycloak and a test SAML IdP) | 11.10(d), 11.100(a), 11.200(a), 11.300(b) | 006 |
| SPEC-020 | Validation documentation | Risk assessment justifying the `Test: yes` selection; IQ/OQ protocol templates with pre-execution approval; tests record expected vs actual results into the SPEC-004 report; compiled OQ test binaries (`go test -c`) runnable on the installed system without source | Expected and actual results captured for every `Test: yes` requirement | 11.10(a); GAMP 5 | 004, 008 |

### Phase 2: Structured data and schema governance

| ID | Title | Scope | Test | Regulatory | Deps |
|---|---|---|---|---|---|
| SPEC-009 | Schema registry and schema-driven forms | JSON Schema 2020-12 versions, immutable once published; `schema_version_id` on nodes and runs; validation of attrs and params on write. UI: forms rendered and validated from the schema, schema browser | Published versions immutable; non-conforming writes rejected | 11.10(h) | 005 |
| SPEC-010 | Schema governance namespaces | §3 above: namespaces, levels, Steward, per-level compatibility linter, change requests as change sets, Controlled changes needing Steward + QA signatures and impact analysis, CURIE fields, optional GitOps. UI: change-request review with diff and impact list | Each level's rule; Controlled needs both signatures; a sandbox never alters its parent | 11.10(k) change control | 007, 008, 009 |
| SPEC-011 | Query and comparison | Filter nodes by kind, schema and JSONB path; page readings by dataset; compare datasets across experiments. UI: search and a comparison view with charts | None (reads, RLS-scoped) | none | 009 |

### Phase 3: Zero-entry ingestion

| ID | Title | Scope | Test | Regulatory | Deps |
|---|---|---|---|---|---|
| SPEC-012 | Raw file custody | Content-addressed original-file store (filesystem first; S3 gets its own decision); SHA-256-pinned `raw_file` node `wasDerivedFrom`-linked to parsed datasets. UI: drag-and-drop upload, viewer and download | Stored bytes re-hash to the recorded digest; originals are immutable | 11.10(b), (c) | 002 |
| SPEC-013 | Parser workers and ingest review | Out-of-process worker contract (claim raw file → submit change set); drop-folder watcher; one reference parser (e.g. Vi-CELL export); sample matching with confidence, never auto-committed. UI: ingest review queue | Worker output can't commit without a human | chain of custody | 007, 012 |

### Phase 4: Agent-native

| ID | Title | Scope | Test | Regulatory | Deps |
|---|---|---|---|---|---|
| SPEC-014 | MCP server | Streamable-HTTP MCP endpoint in the same binary. Resources: lineage, protocols, schemas, audit. Tools: read, validate, propose change sets. Prompts included. Agent principals only; no commit or sign tool | Agent actions audited with `on_behalf_of`; no path to commit or sign | 11.10(d), (e) | 006, 007 |
| SPEC-015 | Inventory and equipment status | Reagent lots (quantity, expiry) and calibration/maintenance status as schema-backed nodes. UI: inventory and equipment views | None | none | 009 |
| SPEC-016 | Pre-flight checks v1 | Preflight endpoint plus a gate on `planned→running`: params vs schema, instrument calibration, lot expiry; results recorded; overrides need a reason. UI: problems panel | Expired lot or out-of-calibration instrument blocks the start unless overridden with a reason | 11.10(f) | 003, 015 |

### Phase 5: Sovereignty and hardening

| ID | Title | Scope | Test | Regulatory | Deps |
|---|---|---|---|---|---|
| SPEC-017 | Open exports | PROV-O JSON-LD lineage, Parquet readings, protocol bundle, tenant archive with a hash manifest | Archive manifest verifies | 11.10(b) | 003, 012 |
| SPEC-018 | Chain anchoring | Signed head checkpoints plus optional RFC 3161 timestamps; the verifier checks against checkpoints; digest v1 unchanged | Checkpoint mismatch detected | 11.10(e) | 001 |
| SPEC-019 | CLI (optional) | `labrat` subcommands for automation: push protocols and schemas, ingest, verify, validate | None | none | 006 |

## 6. Critical path

```
002 Sealing ─┐
003 Protocols ┼─► 006 Identity ─► 007 Revisions ─► 008 Signatures ─► regulated pilot
005 UI shell ─┘        └─► 021 Broker & IdP recipes ──────────┘ (pilot sites on SAML-only or fiddly IdPs)
                                                         │
004 Traceability harness grows alongside every spec       └─► 009 Schemas ─► 010 Governance
```

Ingestion (012–013) and agents (014–016) follow once a trustworthy signature and change-set flow exist. Agents and parsers both depend on change sets.
