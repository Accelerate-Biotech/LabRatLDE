package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
	"github.com/Accelerate-Biotech/LabRatLDE/pkg/frontmatter"
)

type protocolVersion struct {
	ID           string          `json:"id"`
	ExperimentID string          `json:"experiment_id"`
	Version      int             `json:"version"`
	SHA256       string          `json:"sha256"`
	NodeID       string          `json:"node_id"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	ProtocolMD   string          `json:"protocol_md,omitempty"`
	ProtocolMeta json.RawMessage `json:"protocol_meta,omitempty"`
}

// addProtocolVersion parses md, creates the protocol lineage node and the next version of experiment expID, and audits it. Used by createExperiment (v1) and createProtocolVersion.
func addProtocolVersion(ctx context.Context, tx pgx.Tx, p Principal, expID, slug, md string) (protocolVersion, error) {
	if md == "" {
		return protocolVersion{}, badRequest("protocol_md is required")
	}
	doc, err := frontmatter.Parse([]byte(md))
	if err != nil {
		return protocolVersion{}, badRequest("protocol_md: %v", err)
	}
	meta, err := json.Marshal(doc.Meta)
	if err != nil {
		return protocolVersion{}, badRequest("protocol_md frontmatter is not JSON-representable: %v", err)
	}

	// Serialise versions of one experiment so the next number read below can't go stale.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('lde.protocol:' || $1, 0))`, expID); err != nil {
		return protocolVersion{}, err
	}
	var last protocolVersion
	err = tx.QueryRow(ctx, `
		SELECT version, encode(sha256, 'hex') FROM protocol_versions
		WHERE tenant_id = $1 AND experiment_id = $2
		ORDER BY version DESC LIMIT 1`, p.Tenant, expID,
	).Scan(&last.Version, &last.SHA256)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return protocolVersion{}, err
	}
	sum := sha256.Sum256([]byte(md))
	if last.Version > 0 && last.SHA256 == hex.EncodeToString(sum[:]) {
		return protocolVersion{}, &httpError{http.StatusConflict, fmt.Sprintf("protocol text is identical to version %d", last.Version)}
	}

	next := last.Version + 1
	nodeID, err := insertNode(ctx, tx, p, domain.KindProtocol, fmt.Sprintf("%s v%d", slug, next), nil)
	if err != nil {
		return protocolVersion{}, err
	}
	pv := protocolVersion{ExperimentID: expID, NodeID: nodeID, CreatedBy: p.Actor}
	if err := tx.QueryRow(ctx, `
		INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, protocol_meta, node_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, version, encode(sha256, 'hex'), created_at`,
		p.Tenant, expID, next, md, json.RawMessage(meta), nodeID, p.Actor,
	).Scan(&pv.ID, &pv.Version, &pv.SHA256, &pv.CreatedAt); err != nil {
		return protocolVersion{}, err
	}
	_, err = audit.Append(ctx, tx, p.Tenant, audit.Entry{
		Actor: p.Actor, Action: "protocol.version_created", EntityType: "protocol_version", EntityID: pv.ID,
		Payload: map[string]any{"experiment_id": expID, "node_id": nodeID, "sha256": pv.SHA256, "version": pv.Version},
	})
	return pv, err
}

// createProtocolVersion appends a new version to an experiment's protocol.
func (s *Server) createProtocolVersion(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	expID := r.PathValue("id")
	var req struct {
		ProtocolMD string `json:"protocol_md"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if !validUUID(expID) {
		s.fail(w, r, notFound("experiment"))
		return
	}
	var pv protocolVersion
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		var slug string
		if err := tx.QueryRow(ctx, `SELECT slug FROM experiments WHERE id = $1`, expID).Scan(&slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("experiment")
			}
			return err
		}
		var err error
		pv, err = addProtocolVersion(ctx, tx, p, expID, slug, req.ProtocolMD)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, pv)
}

// listProtocolVersions returns an experiment's versions in order, without their text.
func (s *Server) listProtocolVersions(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	expID := r.PathValue("id")
	if !validUUID(expID) {
		s.fail(w, r, notFound("experiment"))
		return
	}
	list := []protocolVersion{}
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, experiment_id, version, encode(sha256, 'hex'), node_id, created_by, created_at
			FROM protocol_versions WHERE experiment_id = $1 ORDER BY version`, expID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var pv protocolVersion
			if err := rows.Scan(&pv.ID, &pv.ExperimentID, &pv.Version, &pv.SHA256, &pv.NodeID, &pv.CreatedBy, &pv.CreatedAt); err != nil {
				return err
			}
			list = append(list, pv)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// getProtocolVersion returns one version, including its text and frontmatter.
func (s *Server) getProtocolVersion(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("protocol version"))
		return
	}
	var pv protocolVersion
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, experiment_id, version, protocol_md, protocol_meta, encode(sha256, 'hex'), node_id, created_by, created_at
			FROM protocol_versions WHERE id = $1`, id,
		).Scan(&pv.ID, &pv.ExperimentID, &pv.Version, &pv.ProtocolMD, &pv.ProtocolMeta, &pv.SHA256, &pv.NodeID, &pv.CreatedBy, &pv.CreatedAt)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}
