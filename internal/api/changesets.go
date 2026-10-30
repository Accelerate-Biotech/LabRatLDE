package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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

// changeSetBody is the body of create and replace.
type changeSetBody struct {
	Title  string       `json:"title"`
	Reason string       `json:"reason"`
	Items  []changeItem `json:"items"`
}

// check rejects a blank title or reason, and items that don't validate.
func (b changeSetBody) check() error {
	if strings.TrimSpace(b.Title) == "" {
		return badRequest("title must not be blank")
	}
	if strings.TrimSpace(b.Reason) == "" {
		return badRequest("reason must not be blank")
	}
	return validateItems(b.Items)
}

// target names the record an item edits, or "" for an item that creates one.
func (it changeItem) target() (entityType, id string) {
	switch it.Op {
	case "update_node":
		return "lineage_node", it.NodeID
	case "update_run_params":
		return "run", it.RunID
	case "set_experiment_title":
		return "experiment", it.ExperimentID
	case "edit_note":
		return "note", it.NoteID
	}
	return "", ""
}

// currentValue returns the latest revision value of the record an item
// edits, or null when the item creates something.
func currentValue(ctx context.Context, tx pgx.Tx, it changeItem) (json.RawMessage, error) {
	entityType, id := it.target()
	if entityType == "" {
		return json.RawMessage("null"), nil
	}
	var v json.RawMessage
	err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT value FROM record_revisions
		                 WHERE entity_type = $1 AND entity_id = $2
		                 ORDER BY rev DESC LIMIT 1), 'null'::jsonb)`, entityType, id).Scan(&v)
	return v, err
}

// createChangeSet proposes a change set. Nothing applies until it is
// committed (SPEC-007-R05).
//
//	POST /v1/change-sets  {"title", "reason", "items"}
func (s *Server) createChangeSet(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var req changeSetBody
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := req.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	raw, err := json.Marshal(req.Items)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var cs changeSet
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO change_sets AS c (tenant_id, title, reason, author, state, items)
			VALUES ($1, $2, $3, $4, 'proposed', $5)
			RETURNING `+changeSetColumns, p.Tenant, req.Title, req.Reason, p.Actor, raw)
		var err error
		if cs, err = scanChangeSet(row); err != nil {
			return err
		}
		_, err = audit.Append(ctx, tx, p.Tenant, audit.Entry{
			Actor: p.Actor, Action: "change_set.proposed", EntityType: "change_set", EntityID: cs.ID, Reason: req.Reason,
			Payload: map[string]any{"items_sha256": cs.ItemsSHA256, "items": len(cs.Items)},
		})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, cs)
}

// listChangeSets pages through change set summaries, oldest first. The
// cursor is the id of the last change set on the previous page.
//
//	GET /v1/change-sets?state=&source=&cursor=<uuid>&limit=<1..200>
func (s *Server) listChangeSets(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	cursor, limit, err := readPage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	state, source := r.URL.Query().Get("state"), r.URL.Query().Get("source")
	if state != "" && !slices.Contains([]string{"proposed", "committed", "rejected", "withdrawn"}, state) {
		s.fail(w, r, badRequest("unknown state %q", state))
		return
	}
	if source != "" && !slices.Contains([]string{"human", "parser", "agent"}, source) {
		s.fail(w, r, badRequest("unknown source %q", source))
		return
	}
	type summary struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Reason    string    `json:"reason"`
		Source    string    `json:"source"`
		Author    string    `json:"author"`
		State     string    `json:"state"`
		Items     int       `json:"items"`
		CreatedAt time.Time `json:"created_at"`
	}
	items := []summary{}
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT c.id, c.title, c.reason, c.source, c.author, c.state,
			       jsonb_array_length(c.items), c.created_at
			FROM change_sets c
			WHERE ($1::text = '' OR c.state = $1)
			  AND ($2::text = '' OR c.source = $2)
			  AND ($3::text = '' OR (c.created_at, c.id) > (SELECT created_at, id FROM change_sets WHERE id = NULLIF($3::text, '')::uuid))
			ORDER BY c.created_at, c.id
			LIMIT $4`, state, source, cursor, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cs summary
			if err := rows.Scan(&cs.ID, &cs.Title, &cs.Reason, &cs.Source, &cs.Author, &cs.State, &cs.Items, &cs.CreatedAt); err != nil {
				return err
			}
			items = append(items, cs)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var last string
	if n := len(items); n > 0 {
		last = items[n-1].ID
	}
	writeJSON(w, http.StatusOK, page{Items: items, NextCursor: nextCursor(len(items), limit, last)})
}

// changeSetDetail is a change set plus, per item, the current value of the
// record it edits.
type changeSetDetail struct {
	changeSet
	Current []json.RawMessage `json:"current"`
}

// getChangeSet returns one change set with its items' current values.
//
//	GET /v1/change-sets/{id}
func (s *Server) getChangeSet(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("change set"))
		return
	}
	var out changeSetDetail
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		cs, err := scanChangeSet(tx.QueryRow(ctx, `SELECT `+changeSetColumns+` FROM change_sets c WHERE c.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("change set")
		} else if err != nil {
			return err
		}
		out.changeSet = cs
		out.Current = make([]json.RawMessage, 0, len(cs.Items))
		for _, it := range cs.Items {
			cur, err := currentValue(ctx, tx, it)
			if err != nil {
				return err
			}
			out.Current = append(out.Current, cur)
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// replaceChangeSet replaces the title, reason and items of a proposed change
// set. The database lets only its author do so (SPEC-007-R12).
//
//	PUT /v1/change-sets/{id}  {"title", "reason", "items"}
func (s *Server) replaceChangeSet(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("change set"))
		return
	}
	var req changeSetBody
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := req.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	raw, err := json.Marshal(req.Items)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var cs changeSet
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE change_sets AS c SET title = $2, reason = $3, items = $4
			WHERE c.id = $1
			RETURNING `+changeSetColumns, id, req.Title, req.Reason, raw)
		var err error
		cs, err = scanChangeSet(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("change set")
		} else if err != nil {
			return err
		}
		_, err = audit.Append(ctx, tx, p.Tenant, audit.Entry{
			Actor: p.Actor, Action: "change_set.updated", EntityType: "change_set", EntityID: cs.ID, Reason: req.Reason,
			Payload: map[string]any{"items_sha256": cs.ItemsSHA256, "items": len(cs.Items)},
		})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}
