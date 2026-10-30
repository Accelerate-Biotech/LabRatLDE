package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
)

// note is a free-text note on a lineage node. Rev is its latest revision.
type note struct {
	ID         string    `json:"id"`
	NodeID     string    `json:"node_id"`
	Body       string    `json:"body"`
	Annotation bool      `json:"annotation"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	Rev        int       `json:"rev"`
}

// insertNote adds a note to a lineage node and audits it. The database marks
// a note on a sealed node as an annotation.
func insertNote(ctx context.Context, tx pgx.Tx, p Principal, nodeID, body string) (note, error) {
	n := note{Rev: 1}
	if err := tx.QueryRow(ctx, `
		INSERT INTO notes (tenant_id, node_id, body, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, node_id, body, annotation, created_by, created_at`,
		p.Tenant, nodeID, body, p.Actor,
	).Scan(&n.ID, &n.NodeID, &n.Body, &n.Annotation, &n.CreatedBy, &n.CreatedAt); err != nil {
		return note{}, err
	}
	_, err := audit.Append(ctx, tx, p.Tenant, audit.Entry{
		Actor: p.Actor, Action: "note.created", EntityType: "note", EntityID: n.ID,
		Payload: map[string]any{"node_id": n.NodeID, "annotation": n.Annotation},
	})
	return n, err
}

// patchNode edits a lineage node's label and/or attrs as a direct change set.
//
//	PATCH /v1/lineage/nodes/{id}   {"label": "...", "attrs": {...}, "reason": "..."}
func (s *Server) patchNode(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("lineage node"))
		return
	}
	var req struct {
		Label  *string        `json:"label"`
		Attrs  map[string]any `json:"attrs"`
		Reason string         `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	it := changeItem{Op: "update_node", NodeID: id, Label: req.Label, Attrs: req.Attrs}
	if err := it.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var res editResult
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		res, err = directEdit(ctx, tx, p, "lineage_node", id, req.Reason, it)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// patchRunParams replaces a run's params as a direct change set. Params can
// change until the run is sealed.
//
//	PATCH /v1/runs/{id}/params   {"params": {...}, "reason": "..."}
func (s *Server) patchRunParams(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("run"))
		return
	}
	var req struct {
		Params json.RawMessage `json:"params"`
		Reason string          `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	it := changeItem{Op: "update_run_params", RunID: id, Params: req.Params}
	if err := it.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var res editResult
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		res, err = directEdit(ctx, tx, p, "run", id, req.Reason, it)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// patchExperiment sets an experiment's title as a direct change set.
//
//	PATCH /v1/experiments/{id}   {"title": "...", "reason": "..."}
func (s *Server) patchExperiment(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("experiment"))
		return
	}
	var req struct {
		Title  string `json:"title"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	it := changeItem{Op: "set_experiment_title", ExperimentID: id, Title: req.Title}
	if err := it.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var res editResult
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		res, err = directEdit(ctx, tx, p, "experiment", id, req.Reason, it)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// patchNote replaces a note's body as a direct change set. The note's rev
// rises by one.
//
//	PATCH /v1/notes/{id}   {"body": "...", "reason": "..."}
func (s *Server) patchNote(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("note"))
		return
	}
	var req struct {
		Body   string `json:"body"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	it := changeItem{Op: "edit_note", NoteID: id, Body: req.Body}
	if err := it.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var res editResult
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		res, err = directEdit(ctx, tx, p, "note", id, req.Reason, it)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// createNote adds a note to a lineage node. It is not an edit, so it makes
// no change set.
//
//	POST /v1/lineage/nodes/{id}/notes   {"body": "..."}
func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("lineage node"))
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		s.fail(w, r, badRequest("body must not be blank"))
		return
	}
	var n note
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		n, err = insertNote(ctx, tx, p, id, req.Body)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

// listNotes returns a node's notes, oldest first, each with its latest rev.
//
//	GET /v1/lineage/nodes/{id}/notes
func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("lineage node"))
		return
	}
	items := []note{}
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT n.id, n.node_id, n.body, n.annotation, n.created_by, n.created_at,
			       (SELECT max(r.rev) FROM record_revisions r WHERE r.entity_type = 'note' AND r.entity_id = n.id)
			FROM notes n
			WHERE n.node_id = $1
			ORDER BY n.created_at, n.id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n note
			if err := rows.Scan(&n.ID, &n.NodeID, &n.Body, &n.Annotation, &n.CreatedBy, &n.CreatedAt, &n.Rev); err != nil {
				return err
			}
			items = append(items, n)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []note `json:"items"`
	}{items})
}
