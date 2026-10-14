package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
	"github.com/Accelerate-Biotech/LabRatLDE/pkg/frontmatter"
)

type experiment struct {
	ID           string          `json:"id"`
	Slug         string          `json:"slug"`
	Title        string          `json:"title"`
	ProtocolMD   string          `json:"protocol_md"`
	ProtocolMeta json.RawMessage `json:"protocol_meta"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
}

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
	doc, err := frontmatter.Parse([]byte(req.ProtocolMD))
	if err != nil {
		s.fail(w, r, badRequest("protocol_md: %v", err))
		return
	}
	meta, err := json.Marshal(doc.Meta)
	if err != nil {
		s.fail(w, r, badRequest("protocol_md frontmatter is not JSON-representable: %v", err))
		return
	}

	exp := experiment{Slug: req.Slug, Title: req.Title, ProtocolMD: req.ProtocolMD, ProtocolMeta: meta, CreatedBy: p.Actor}
	err = s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO experiments (tenant_id, slug, title, protocol_md, protocol_meta, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, created_at`,
			p.Tenant, exp.Slug, exp.Title, exp.ProtocolMD, exp.ProtocolMeta, p.Actor,
		).Scan(&exp.ID, &exp.CreatedAt); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(exp.ProtocolMD))
		_, err := audit.Append(ctx, tx, p.Tenant, audit.Entry{
			Actor: p.Actor, Action: "experiment.created", EntityType: "experiment", EntityID: exp.ID,
			Payload: map[string]any{"slug": exp.Slug, "title": exp.Title, "protocol_sha256": hex.EncodeToString(digest[:])},
		})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, exp)
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
			SELECT id, slug, title, protocol_md, protocol_meta, created_by, created_at
			FROM experiments WHERE id = $1`, id,
		).Scan(&exp.ID, &exp.Slug, &exp.Title, &exp.ProtocolMD, &exp.ProtocolMeta, &exp.CreatedBy, &exp.CreatedAt)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, exp)
}
