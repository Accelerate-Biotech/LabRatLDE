package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
)

// experiment is read together with its highest protocol version.
type experiment struct {
	ID                string          `json:"id"`
	Slug              string          `json:"slug"`
	Title             string          `json:"title"`
	ProtocolVersionID string          `json:"protocol_version_id"`
	ProtocolVersion   int             `json:"protocol_version"`
	ProtocolSHA256    string          `json:"protocol_sha256"`
	ProtocolMD        string          `json:"protocol_md"`
	ProtocolMeta      json.RawMessage `json:"protocol_meta"`
	CreatedBy         string          `json:"created_by"`
	CreatedAt         time.Time       `json:"created_at"`
}

// createExperiment creates an experiment and version 1 of its protocol in one
// transaction.
func (s *Server) createExperiment(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var req struct {
		Slug       string `json:"slug"`
		Title      string `json:"title"`
		ProtocolMD string `json:"protocol_md"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Slug == "" || req.Title == "" {
		s.fail(w, r, badRequest("slug and title are required"))
		return
	}
	if req.ProtocolMD == "" {
		s.fail(w, r, badRequest("protocol_md is required"))
		return
	}

	exp := experiment{Slug: req.Slug, Title: req.Title, ProtocolMD: req.ProtocolMD, CreatedBy: p.Actor}
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO experiments (tenant_id, slug, title, created_by)
			VALUES ($1, $2, $3, $4)
			RETURNING id, created_at`,
			p.Tenant, exp.Slug, exp.Title, p.Actor,
		).Scan(&exp.ID, &exp.CreatedAt); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, tx, p.Tenant, audit.Entry{
			Actor: p.Actor, Action: "experiment.created", EntityType: "experiment", EntityID: exp.ID,
			Payload: map[string]any{"slug": exp.Slug, "title": exp.Title},
		}); err != nil {
			return err
		}
		pv, err := addProtocolVersion(ctx, tx, p, exp.ID, exp.Slug, req.ProtocolMD)
		if err != nil {
			return err
		}
		exp.ProtocolVersionID, exp.ProtocolVersion, exp.ProtocolSHA256 = pv.ID, pv.Version, pv.SHA256
		return tx.QueryRow(ctx, `SELECT protocol_meta FROM protocol_versions WHERE id = $1`, pv.ID).Scan(&exp.ProtocolMeta)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, exp)
}

// listExperiments pages through experiments in slug order (slug is unique).
// The cursor is the id of the last experiment on the previous page.
//
//	GET /v1/experiments?cursor=<uuid>&limit=<1..200>
func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	cursor, limit, err := readPage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type summary struct {
		ID        string    `json:"id"`
		Slug      string    `json:"slug"`
		Title     string    `json:"title"`
		CreatedAt time.Time `json:"created_at"`
	}
	items := []summary{}
	err = s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, slug, title, created_at FROM experiments
			WHERE ($1::text = '' OR slug > (SELECT slug FROM experiments WHERE id = NULLIF($1::text, '')::uuid))
			ORDER BY slug
			LIMIT $2`, cursor, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e summary
			if err := rows.Scan(&e.ID, &e.Slug, &e.Title, &e.CreatedAt); err != nil {
				return err
			}
			items = append(items, e)
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

func (s *Server) getExperiment(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("experiment"))
		return
	}
	var exp experiment
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT e.id, e.slug, e.title, v.id, v.version, encode(v.sha256, 'hex'), v.protocol_md, v.protocol_meta,
			       e.created_by, e.created_at
			FROM experiments e
			JOIN LATERAL (
				SELECT * FROM protocol_versions v
				WHERE v.tenant_id = e.tenant_id AND v.experiment_id = e.id
				ORDER BY v.version DESC LIMIT 1
			) v ON true
			WHERE e.id = $1`, id,
		).Scan(&exp.ID, &exp.Slug, &exp.Title, &exp.ProtocolVersionID, &exp.ProtocolVersion, &exp.ProtocolSHA256,
			&exp.ProtocolMD, &exp.ProtocolMeta, &exp.CreatedBy, &exp.CreatedAt)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, exp)
}
