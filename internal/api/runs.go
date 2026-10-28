package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
)

const runColumns = `id, tenant_id::text, experiment_id, protocol_version_id, node_id, state, params, started_at, finished_at, created_by, created_at`

func scanRun(row pgx.Row) (domain.Run, error) {
	var run domain.Run
	err := row.Scan(&run.ID, &run.TenantID, &run.ExperimentID, &run.ProtocolVersionID, &run.NodeID, &run.State, &run.Params,
		&run.StartedAt, &run.FinishedAt, &run.CreatedBy, &run.CreatedAt)
	return run, err
}

// createRun starts a run of an experiment, pinned to one of its protocol
// versions (the highest unless the caller names one). The run becomes a
// lineage node; the protocol version and every input (sample, reagent lot,
// upstream dataset) are linked to it with prov:used in the same transaction.
func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var req struct {
		ExperimentID      string          `json:"experiment_id"`
		ProtocolVersionID string          `json:"protocol_version_id"`
		Label             string          `json:"label"`
		Params            json.RawMessage `json:"params"`
		Inputs            []string        `json:"inputs"` // lineage node ids this run consumes
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if !validUUID(req.ExperimentID) {
		s.fail(w, r, badRequest("experiment_id must be a uuid"))
		return
	}
	if req.ProtocolVersionID != "" && !validUUID(req.ProtocolVersionID) {
		s.fail(w, r, badRequest("protocol_version_id must be a uuid"))
		return
	}
	for _, in := range req.Inputs {
		if !validUUID(in) {
			s.fail(w, r, badRequest("inputs must be lineage node uuids"))
			return
		}
	}
	if len(req.Params) == 0 {
		req.Params = json.RawMessage(`{}`)
	}
	if req.Label == "" {
		req.Label = "run"
	}

	var run domain.Run
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		var pv protocolVersion
		err := tx.QueryRow(ctx, `
			SELECT id, version, encode(sha256, 'hex'), node_id FROM protocol_versions
			WHERE experiment_id = $1 AND ($2 = '' OR id::text = $2)
			ORDER BY version DESC LIMIT 1`, req.ExperimentID, req.ProtocolVersionID,
		).Scan(&pv.ID, &pv.Version, &pv.SHA256, &pv.NodeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("protocol version")
		}
		if err != nil {
			return err
		}
		nodeID, err := insertNode(ctx, tx, p, domain.KindRun, req.Label, nil)
		if err != nil {
			return err
		}
		run, err = scanRun(tx.QueryRow(ctx, `
			INSERT INTO runs (tenant_id, experiment_id, protocol_version_id, node_id, params, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING `+runColumns,
			p.Tenant, req.ExperimentID, pv.ID, nodeID, req.Params, p.Actor))
		if err != nil {
			return err
		}
		if err := insertEdge(ctx, tx, p, domain.Edge{From: pv.NodeID, To: nodeID, Relation: domain.RelUsed}); err != nil {
			return err
		}
		for _, in := range req.Inputs {
			if err := insertEdge(ctx, tx, p, domain.Edge{From: in, To: nodeID, Relation: domain.RelUsed}); err != nil {
				return err
			}
		}
		_, err = audit.Append(ctx, tx, p.Tenant, audit.Entry{
			Actor: p.Actor, Action: "run.created", EntityType: "run", EntityID: run.ID,
			Payload: map[string]any{"experiment_id": run.ExperimentID, "node_id": nodeID, "params": run.Params, "inputs": req.Inputs,
				"protocol_version_id": pv.ID, "protocol_version": pv.Version, "protocol_sha256": pv.SHA256},
		})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

type runInput struct {
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
}

type runDataset struct {
	NodeID string  `json:"node_id"`
	Label  string  `json:"label"`
	Count  *int64  `json:"count"`
	SHA256 *string `json:"sha256"`
}

type runDetail struct {
	domain.Run
	ProtocolVersion int          `json:"protocol_version"`
	ProtocolSHA256  string       `json:"protocol_sha256"`
	Inputs          []runInput   `json:"inputs"`
	Datasets        []runDataset `json:"datasets"`
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("run"))
		return
	}
	detail := runDetail{Inputs: []runInput{}, Datasets: []runDataset{}}
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		detail.Run, err = scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1`, id))
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			SELECT version, encode(sha256, 'hex') FROM protocol_versions WHERE id = $1`, detail.ProtocolVersionID,
		).Scan(&detail.ProtocolVersion, &detail.ProtocolSHA256)
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			SELECT n.id, n.kind, n.label FROM lineage_edges e
			JOIN lineage_nodes n ON n.id = e.from_id
			WHERE e.to_id = $1 AND e.relation = 'used' AND n.kind <> 'protocol'
			ORDER BY n.label`, detail.NodeID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var in runInput
			if err := rows.Scan(&in.NodeID, &in.Kind, &in.Label); err != nil {
				rows.Close()
				return err
			}
			detail.Inputs = append(detail.Inputs, in)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			SELECT n.id, n.label, (a.payload::jsonb->>'count')::bigint, a.payload::jsonb->>'sha256'
			FROM lineage_edges e
			JOIN lineage_nodes n ON n.id = e.to_id
			LEFT JOIN audit_events a ON a.entity_type = 'dataset' AND a.entity_id = n.id::text AND a.action = 'instrument.ingested'
			WHERE e.from_id = $1 AND e.relation = 'wasGeneratedBy'
			ORDER BY n.label`, detail.NodeID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var ds runDataset
			if err := rows.Scan(&ds.NodeID, &ds.Label, &ds.Count, &ds.SHA256); err != nil {
				rows.Close()
				return err
			}
			detail.Datasets = append(detail.Datasets, ds)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// listExperimentRuns pages through an experiment's runs in creation order.
// The cursor is the id of the last run on the previous page.
//
//	GET /v1/experiments/{id}/runs?cursor=<uuid>&limit=<1..200>
func (s *Server) listExperimentRuns(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("experiment"))
		return
	}
	cursor, limit, err := readPage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type summary struct {
		ID        string    `json:"id"`
		Label     string    `json:"label"`
		State     string    `json:"state"`
		CreatedAt time.Time `json:"created_at"`
	}
	items := []summary{}
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM experiments WHERE id = $1`, id).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("experiment")
		} else if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT r.id, n.label, r.state, r.created_at FROM runs r
			JOIN lineage_nodes n ON n.id = r.node_id
			WHERE r.experiment_id = $1
			  AND ($2::text = '' OR (r.created_at, r.id) > (SELECT created_at, id FROM runs WHERE id = NULLIF($2::text, '')::uuid))
			ORDER BY r.created_at, r.id
			LIMIT $3`, id, cursor, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run summary
			if err := rows.Scan(&run.ID, &run.Label, &run.State, &run.CreatedAt); err != nil {
				return err
			}
			items = append(items, run)
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

// transitionRun advances the run state machine. Transitioning to "signed" is
// an electronic signature: reason carries its meaning (§11.50(a)(3)).
//
// TODO(part11): §11.200 requires signing to re-authenticate the signer
// (e.g. an OIDC step-up with max_age=0). Wire that in with real auth.
func (s *Server) transitionRun(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("run"))
		return
	}
	var req struct {
		To     domain.RunState `json:"to"`
		Reason string          `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if !req.To.Valid() {
		s.fail(w, r, badRequest("unknown state %q", req.To))
		return
	}

	var run domain.Run
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		var err error
		run, err = scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		from := run.State
		if err := run.Apply(req.To, req.Reason, time.Now().UTC()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE runs SET state = $2, started_at = $3, finished_at = $4 WHERE id = $1`,
			run.ID, run.State, run.StartedAt, run.FinishedAt); err != nil {
			return err
		}
		return appendRunEvent(ctx, tx, p, run.ID, from, run.State, req.Reason)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func appendRunEvent(ctx context.Context, tx pgx.Tx, p Principal, runID string, from, to domain.RunState, reason string) error {
	action := "run.transitioned"
	if to == domain.RunSigned {
		action = "run.signed"
	}
	_, err := audit.Append(ctx, tx, p.Tenant, audit.Entry{
		Actor: p.Actor, Action: action, EntityType: "run", EntityID: runID, Reason: reason,
		Payload: map[string]any{"from": from, "to": to},
	})
	return err
}
