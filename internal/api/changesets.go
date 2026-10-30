package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
)

// changeItem is one edit inside a change set. Each op reads only the fields
// it names; validate says which.
type changeItem struct {
	Op           string          `json:"op"`
	NodeID       string          `json:"node_id,omitempty"`
	RunID        string          `json:"run_id,omitempty"`
	ExperimentID string          `json:"experiment_id,omitempty"`
	NoteID       string          `json:"note_id,omitempty"`
	Kind         domain.NodeKind `json:"kind,omitempty"`
	Label        *string         `json:"label,omitempty"`
	// Attrs has no omitempty: an empty object clears attrs, and must survive a round trip.
	Attrs    map[string]any  `json:"attrs"`
	Params   json.RawMessage `json:"params,omitempty"`
	Title    string          `json:"title,omitempty"`
	Body     string          `json:"body,omitempty"`
	From     string          `json:"from,omitempty"`
	To       string          `json:"to,omitempty"`
	Relation domain.Relation `json:"relation,omitempty"`
}

// validate checks an item's fields for its op, so nothing malformed reaches
// the database.
func (it changeItem) validate() error {
	switch it.Op {
	case "update_node":
		if !validUUID(it.NodeID) {
			return badRequest("update_node needs a node_id uuid")
		}
		if it.Label == nil && it.Attrs == nil {
			return badRequest("update_node needs a label or attrs")
		}
		if it.Label != nil && strings.TrimSpace(*it.Label) == "" {
			return badRequest("label must not be blank")
		}
	case "update_run_params":
		if !validUUID(it.RunID) {
			return badRequest("update_run_params needs a run_id uuid")
		}
		var params map[string]any
		if err := json.Unmarshal(it.Params, &params); err != nil || params == nil {
			return badRequest("params must be a JSON object")
		}
	case "set_experiment_title":
		if !validUUID(it.ExperimentID) {
			return badRequest("set_experiment_title needs an experiment_id uuid")
		}
		if strings.TrimSpace(it.Title) == "" {
			return badRequest("title must not be blank")
		}
	case "add_node":
		if !it.Kind.Valid() {
			return badRequest("unknown node kind %q", it.Kind)
		}
		if it.Label == nil || strings.TrimSpace(*it.Label) == "" {
			return badRequest("add_node needs a label")
		}
	case "add_edge":
		if !validUUID(it.From) || !validUUID(it.To) {
			return badRequest("add_edge needs from and to lineage node uuids")
		}
		if !it.Relation.Valid() {
			return badRequest("unknown relation %q", it.Relation)
		}
	case "add_note":
		if !validUUID(it.NodeID) {
			return badRequest("add_note needs a node_id uuid")
		}
		if strings.TrimSpace(it.Body) == "" {
			return badRequest("body must not be blank")
		}
	case "edit_note":
		if !validUUID(it.NoteID) {
			return badRequest("edit_note needs a note_id uuid")
		}
		if strings.TrimSpace(it.Body) == "" {
			return badRequest("body must not be blank")
		}
	default:
		return badRequest("unknown op %q", it.Op)
	}
	return nil
}

// validateItems checks a change set's items: 1 to 100 of them, each valid.
func validateItems(items []changeItem) error {
	if len(items) < 1 || len(items) > 100 {
		return badRequest("a change set needs 1 to 100 items, got %d", len(items))
	}
	for i, it := range items {
		if err := it.validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	return nil
}

// latestRev returns the highest revision of a record, or 0 if it has none.
func latestRev(ctx context.Context, tx pgx.Tx, entityType, id string) (int, error) {
	var rev int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(rev), 0) FROM record_revisions
		WHERE entity_type = $1 AND entity_id = $2`, entityType, id).Scan(&rev)
	return rev, err
}

// applyItems applies a change set's items in order. The change set must
// already be committed in tx: the revision trigger accepts only that.
func applyItems(ctx context.Context, tx pgx.Tx, p Principal, csID, reason string, items []changeItem) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.change_set_id', $1, true)`, csID); err != nil {
		return err
	}
	for _, it := range items {
		if err := applyItem(ctx, tx, p, csID, reason, it); err != nil {
			return err
		}
	}
	return nil
}

// applyItem applies one item of change set csID.
func applyItem(ctx context.Context, tx pgx.Tx, p Principal, csID, reason string, it changeItem) error {
	switch it.Op {
	case "update_node":
		return updateRecord(ctx, tx, p, csID, reason, "lineage_node", it.NodeID,
			`UPDATE lineage_nodes SET label = COALESCE($2, label), attrs = COALESCE($3, attrs) WHERE id = $1`,
			it.NodeID, it.Label, it.Attrs)
	case "update_run_params":
		return updateRecord(ctx, tx, p, csID, reason, "run", it.RunID,
			`UPDATE runs SET params = $2 WHERE id = $1`, it.RunID, it.Params)
	case "set_experiment_title":
		return updateRecord(ctx, tx, p, csID, reason, "experiment", it.ExperimentID,
			`UPDATE experiments SET title = $2 WHERE id = $1`, it.ExperimentID, it.Title)
	case "edit_note":
		return updateRecord(ctx, tx, p, csID, reason, "note", it.NoteID,
			`UPDATE notes SET body = $2 WHERE id = $1`, it.NoteID, it.Body)
	case "add_node":
		_, err := insertNode(ctx, tx, p, it.Kind, *it.Label, it.Attrs)
		return err
	case "add_edge":
		return insertEdge(ctx, tx, p, domain.Edge{From: it.From, To: it.To, Relation: it.Relation})
	case "add_note":
		_, err := insertNote(ctx, tx, p, it.NodeID, it.Body)
		return err
	}
	return badRequest("unknown op %q", it.Op)
}

// updateRecord runs one UPDATE of a revisable record. If the trigger wrote a
// new revision, it audits record.revised.
func updateRecord(ctx context.Context, tx pgx.Tx, p Principal, csID, reason, entityType, entityID, query string, args ...any) error {
	before, err := latestRev(ctx, tx, entityType, entityID)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return notFound(entityType)
	}
	after, err := latestRev(ctx, tx, entityType, entityID)
	if err != nil || after == before {
		return err
	}
	_, err = audit.Append(ctx, tx, p.Tenant, audit.Entry{
		Actor: p.Actor, Action: "record.revised", EntityType: entityType, EntityID: entityID, Reason: reason,
		Payload: map[string]any{"rev": after, "change_set_id": csID},
	})
	return err
}

// changeSetColumns selects a change set for scanChangeSet. The decider is
// shown by handle.
const changeSetColumns = `c.id, c.title, c.reason, c.source, c.author, c.state, c.items,
	encode(c.items_sha256, 'hex'), (SELECT handle FROM principals WHERE id = c.decided_by),
	c.decided_at, c.decision_reason, c.created_at`

// changeSet is a change set as the API returns it.
type changeSet struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	Reason         string       `json:"reason"`
	Source         string       `json:"source"`
	Author         string       `json:"author"`
	State          string       `json:"state"`
	Items          []changeItem `json:"items"`
	ItemsSHA256    string       `json:"items_sha256"`
	DecidedBy      *string      `json:"decided_by"`
	DecidedAt      *time.Time   `json:"decided_at"`
	DecisionReason *string      `json:"decision_reason"`
	CreatedAt      time.Time    `json:"created_at"`
}

func scanChangeSet(row pgx.Row) (changeSet, error) {
	var cs changeSet
	err := row.Scan(&cs.ID, &cs.Title, &cs.Reason, &cs.Source, &cs.Author, &cs.State, &cs.Items,
		&cs.ItemsSHA256, &cs.DecidedBy, &cs.DecidedAt, &cs.DecisionReason, &cs.CreatedAt)
	return cs, err
}

// editResult is the answer to a direct edit: the change set that recorded it
// and the record's revision after it.
type editResult struct {
	ChangeSetID string `json:"change_set_id"`
	Rev         int    `json:"rev"`
}

// directEdit records one edit as a change set committed in the same
// transaction, then applies it (SPEC-007-R11). It writes change_set.committed
// before the changes, so the audit trail reads in order.
func directEdit(ctx context.Context, tx pgx.Tx, p Principal, entityType, entityID, reason string, it changeItem) (editResult, error) {
	items := []changeItem{it}
	raw, err := json.Marshal(items)
	if err != nil {
		return editResult{}, err
	}
	var res editResult
	var sha string
	if err := tx.QueryRow(ctx, `
		INSERT INTO change_sets (tenant_id, title, reason, author, state, items, decision_reason)
		VALUES ($1, 'Edit ' || $2, $3, $4, 'committed', $5, $3)
		RETURNING id, encode(items_sha256, 'hex')`,
		p.Tenant, entityType, reason, p.Actor, raw).Scan(&res.ChangeSetID, &sha); err != nil {
		return editResult{}, err
	}
	if _, err := audit.Append(ctx, tx, p.Tenant, audit.Entry{
		Actor: p.Actor, Action: "change_set.committed", EntityType: "change_set", EntityID: res.ChangeSetID, Reason: reason,
		Payload: map[string]any{"items_sha256": sha, "items": len(items)},
	}); err != nil {
		return editResult{}, err
	}
	if err := applyItems(ctx, tx, p, res.ChangeSetID, reason, items); err != nil {
		return editResult{}, err
	}
	res.Rev, err = latestRev(ctx, tx, entityType, entityID)
	return res, err
}
