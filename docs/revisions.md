# Revisions, change sets and notes

Lab records can be corrected, but never overwritten. Each change adds a
**revision** that holds the full new value, and the earlier revisions stay
readable. Every edit goes through a **change set** with a reason. The database
writes the history in the same transaction as the change, so no endpoint or
script can skip it. This is the design of [SPEC-007](../specs/SPEC-007-revisions-and-change-sets/requirements.md),
which is in progress; this page describes what it delivers.

## What can be revised

| Record | `{type}` in the URLs | Revisable fields |
|---|---|---|
| Lineage node | `lineage_node` | `label`, `attrs` |
| Run | `run` | `params` |
| Experiment | `experiment` | `title` |
| Note | `note` | `body` |

Nothing else has revisions. A run's state change, a protocol version, an
instrument reading or an audit event is not revised, so it needs no change set.

Revision 1 is the value a record had when it was created. The creation event
(`lineage.node_created`, `run.created`, `experiment.created` or `note.created`)
covers it. Each later change adds revision 2, 3, and so on, and is audited as
`record.revised`.

## Every edit is a change set

A change set has a title, a required reason and one to 100 **items**. Its items
are applied in order, all or none. Each item is one of:

| `op` | Does |
|---|---|
| `update_node` | sets `label` and/or `attrs` of a lineage node |
| `update_run_params` | replaces a run's `params` |
| `set_experiment_title` | sets an experiment's `title` |
| `edit_note` | replaces a note's `body` |
| `add_node`, `add_edge`, `add_note` | creates a node, an edge or a note |

A **direct edit** is a change set with one item, committed by the same request.
It appears in the same history as any other change. The reason is required, and
a blank one is `400`.

`attrs` replaces the whole object, so send every key you want to keep.

## Lifecycle and who can do what

```
proposed ──► committed    a human scientist; applies every item in one transaction
proposed ──► rejected     a human scientist
proposed ──► withdrawn    the author
```

`committed`, `rejected` and `withdrawn` are final. While a change set is
`proposed`, its items aren't visible in record reads, lineage or signature
manifests. Its author may replace its title, reason and items with `PUT`.

| Action | Who |
|---|---|
| Propose a change set | a principal with the `scientist` role: a human, an agent acting for one, or a service |
| Replace the items of a proposal | its author, while it is `proposed` |
| Withdraw a proposal | its author, while it is `proposed` |
| Commit or reject a proposal | a **human** with the `scientist` role. Agents and services get `403` |
| Direct edit | a **human** with the `scientist` role, since it commits its own change set |

Agents and services propose only. The database sets each change set's `source`
(`human`, `agent` or `parser`) and its `decided_by` from the caller's principal,
so a client can't claim to be a human or make someone else's decision. A
revision credits the change set's author, and the decider is kept on the change
set.

## Sealed records

A signed or aborted run seals its lineage node, the datasets it generated and
its params (SPEC-002). Those records **can't be changed yet**. A change set that
touches one can be proposed, but its commit returns `409`, and so does a direct
edit. Reviewing and re-signing corrections to sealed records is the job of
[SPEC-008](../specs/SPEC-008-electronic-signatures/requirements.md).

Notes follow a different rule:

- A note added to a sealed node is an **annotation**. It is stored with
  `annotation: true`, and the node stays sealed.
- Annotations can be edited like any note.
- Editing a note written before the seal touches a sealed record, so it returns
  `409`.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/change-sets` | Propose `{title, reason, items}`; returns `201` |
| `GET` | `/v1/change-sets?state=&source=&cursor=&limit=` | List summaries, oldest first |
| `GET` | `/v1/change-sets/{id}` | One change set, with each item's current record value in `current` |
| `PUT` | `/v1/change-sets/{id}` | Replace title, reason and items (author, while proposed) |
| `POST` | `/v1/change-sets/{id}/commit` | Commit, with `{reason}` (human scientist) |
| `POST` | `/v1/change-sets/{id}/reject` | Reject, with `{reason}` (human scientist) |
| `POST` | `/v1/change-sets/{id}/withdraw` | Withdraw, with `{reason}` (author) |
| `PATCH` | `/v1/lineage/nodes/{id}` | Direct edit `{label?, attrs?, reason}`; returns `{change_set_id, rev}` |
| `PATCH` | `/v1/runs/{id}/params` | Direct edit `{params, reason}`; returns `{change_set_id, rev}` |
| `PATCH` | `/v1/experiments/{id}` | Direct edit `{title, reason}`; returns `{change_set_id, rev}` |
| `POST` | `/v1/lineage/nodes/{id}/notes` | Add a note `{body}`. Not an edit, so no change set |
| `GET` | `/v1/lineage/nodes/{id}/notes` | A node's notes, oldest first, each with its latest `rev` |
| `PATCH` | `/v1/notes/{id}` | Edit a note `{body, reason}`; returns `{change_set_id, rev}` |
| `GET` | `/v1/records/{type}/{id}/revisions` | Every revision, oldest first |
| `GET` | `/v1/records/{type}/{id}/revisions/{rev}` | One revision, with its full value |
| `GET` | `/v1/records/{type}/{id}/diff?from=&to=` | Field-level diff between two revisions |
| `GET` | `/v1/records/{type}/{id}/blame` | For each field, the revision, author and time that last changed it |

Errors: `400` for a blank reason or an invalid item, `403` when the role or the
author check fails, `409` for a sealed record or a change set that is no longer
`proposed`.

Paths in a diff start at `$`, the revision's value. Keys are written `.key`
(`$.attrs.volume_ul`), array indexes `[i]`.

## Examples

These use the dev headers from the [README walkthrough](../README.md#walkthrough-sample--run--instrument-stream--signature--proof).
In production, send `-H "Authorization: Bearer $TOKEN"` instead; see
[identity.md](identity.md).

```bash
T=00000000-0000-4000-8000-000000000001     # seeded dev tenant
API=localhost:8080/v1
H=(-H "X-LDE-Tenant: $T" -H "X-LDE-Actor: alice@lab.example" -H "Content-Type: application/json")
BOB=(-H "X-LDE-Tenant: $T" -H "X-LDE-Actor: bob@lab.example" -H "Content-Type: application/json")
SAMPLE=<lineage node id>                   # e.g. the sample from the walkthrough
```

Direct edit by alice. The node's `attrs` gets revision 2:

```bash
curl -s "${H[@]}" -X PATCH $API/lineage/nodes/$SAMPLE \
  -d '{"attrs": {"donor": "D-17", "volume_ul": 45}, "reason": "pipetting error"}' | jq
# {"change_set_id": "…", "rev": 2}
```

Proposal by bob. Nothing changes until it is committed:

```bash
CS=$(curl -s "${BOB[@]}" $API/change-sets -d @- <<EOF | jq -r .id
{"title": "Correct plasma volume",
 "reason": "pipetting error on plate 7",
 "items": [{"op": "update_node", "node_id": "$SAMPLE",
            "attrs": {"donor": "D-17", "volume_ul": 45}}]}
EOF
)
```

Commit by alice, a human scientist:

```bash
curl -s "${H[@]}" $API/change-sets/$CS/commit \
  -d '{"reason": "confirmed against the pipette log"}' | jq '{state, decided_by, decision_reason}'
```

Diff between revisions 1 and 2 of the node:

```bash
curl -s "${H[@]}" "$API/records/lineage_node/$SAMPLE/diff?from=1&to=2" | jq
# {"from": 1, "to": 2, "changes": [{"path": "$.attrs.volume_ul", "kind": "added", "to": 45}]}
```
