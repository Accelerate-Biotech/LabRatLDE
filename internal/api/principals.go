package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/identity"
)

// principalWithRoles is a principal and its active grants.
type principalWithRoles struct {
	identity.Principal
	Roles []identity.RoleGrant `json:"roles"`
}

// getMe describes the caller: who they are, who an agent acts for, and the
// roles they hold.
//
//	GET /v1/me
func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var me struct {
		principalWithRoles
		Actor      string  `json:"actor"`
		OnBehalfOf *string `json:"on_behalf_of"`
		Auth       string  `json:"auth"`
	}
	me.Actor, me.Auth = p.Actor, "oidc"
	if s.cfg.Env == "dev" {
		me.Auth = "dev"
	}
	if p.ForHandle != "" {
		me.OnBehalfOf = &p.ForHandle
	}
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		me.principalWithRoles, err = loadPrincipal(r, tx, p.As.ID)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

// listPrincipals pages through the tenant's principals by handle, each with
// its active roles. Any principal may read the directory.
//
//	GET /v1/principals?cursor=<uuid>&limit=<1..200>
func (s *Server) listPrincipals(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	cursor, limit, err := readPage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := []principalWithRoles{}
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id FROM principals
			WHERE ($1::text = '' OR handle > (SELECT handle FROM principals WHERE id = NULLIF($1::text, '')::uuid))
			ORDER BY handle LIMIT $2`, cursor, limit)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			pr, err := loadPrincipal(r, tx, id)
			if err != nil {
				return err
			}
			items = append(items, pr)
		}
		return nil
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

// createPrincipal provisions a principal with no roles. The database lets
// only admins do it.
//
//	POST /v1/principals {kind, handle, display_name, email, issuer, subject, reason}
func (s *Server) createPrincipal(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var req struct {
		Kind        string `json:"kind"`
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Issuer      string `json:"issuer"`
		Subject     string `json:"subject"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	switch {
	case req.Kind != "human" && req.Kind != "service" && req.Kind != "agent":
		s.fail(w, r, badRequest("kind must be human, service or agent"))
		return
	case !identity.ValidHandle(req.Handle):
		s.fail(w, r, badRequest("handle must be lowercase letters, digits and ._@+-, starting with a letter or digit"))
		return
	case strings.TrimSpace(req.Issuer) == "" || strings.TrimSpace(req.Subject) == "":
		s.fail(w, r, badRequest("issuer and subject are required"))
		return
	case strings.TrimSpace(req.Reason) == "":
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	var out principalWithRoles
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		out.Principal, err = identity.Create(ctx, tx, p.Actor, req.Reason, identity.Principal{
			TenantID: p.Tenant, Kind: req.Kind, Handle: req.Handle, DisplayName: req.DisplayName,
			Email: req.Email, Issuer: req.Issuer, Subject: req.Subject,
		})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out.Roles = []identity.RoleGrant{}
	writeJSON(w, http.StatusCreated, out)
}

// disablePrincipal stops a principal from authenticating. Its handle stays
// taken, and its history stays attributed to it.
//
//	POST /v1/principals/{id}/disable {reason}
func (s *Server) disablePrincipal(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("principal"))
		return
	}
	reason, err := readReason(w, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if id == p.As.ID {
		s.fail(w, r, badRequest("you can't disable yourself"))
		return
	}
	var out principalWithRoles
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		if _, err := identity.SetEnabled(ctx, tx, p.Actor, p.Tenant, id, false, reason); err != nil {
			return err
		}
		var err error
		out, err = loadPrincipal(r, tx, id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		err = notFound("principal")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// createRoleGrant grants a role. The database lets only admins do it, and
// stamps the grantor and time.
//
//	POST /v1/role-grants {principal_id, role, scope, reason}
func (s *Server) createRoleGrant(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var req struct {
		PrincipalID string `json:"principal_id"`
		Role        string `json:"role"`
		Scope       string `json:"scope"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	switch {
	case !validUUID(req.PrincipalID):
		s.fail(w, r, badRequest("principal_id must be a uuid"))
		return
	case strings.TrimSpace(req.Reason) == "":
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	var g identity.RoleGrant
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		g, err = identity.Grant(ctx, tx, p.Actor, p.Tenant, req.PrincipalID, req.Role, req.Scope, req.Reason, p.As.ID)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// revokeRoleGrant revokes a grant, once.
//
//	POST /v1/role-grants/{id}/revoke {reason}
func (s *Server) revokeRoleGrant(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("role grant"))
		return
	}
	reason, err := readReason(w, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var g identity.RoleGrant
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		g, err = identity.Revoke(ctx, tx, p.Actor, p.Tenant, id, reason, p.As.ID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		err = notFound("role grant")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// delegationView is an active delegation with both handles.
type delegationView struct {
	identity.Delegation
	AgentHandle string `json:"agent_handle"`
	HumanHandle string `json:"human_handle"`
}

// listDelegations lists the tenant's active delegations, newest first.
//
//	GET /v1/agent-delegations
func (s *Server) listDelegations(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	items := []delegationView{}
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT d.id, d.agent_id, d.human_id, d.reason, d.created_by, d.created_at, d.ended_by, d.ended_at,
			       a.handle, h.handle
			FROM agent_delegations d
			JOIN principals a ON a.id = d.agent_id
			JOIN principals h ON h.id = d.human_id
			WHERE d.ended_at IS NULL
			ORDER BY d.created_at DESC, d.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v delegationView
			if err := rows.Scan(&v.ID, &v.AgentID, &v.HumanID, &v.Reason, &v.CreatedBy, &v.CreatedAt, &v.EndedBy,
				&v.EndedAt, &v.AgentHandle, &v.HumanHandle); err != nil {
				return err
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// createDelegation lets an agent act for a human. The database lets only
// admins do it, and checks the two principals' kinds.
//
//	POST /v1/agent-delegations {agent_id, human_id, reason}
func (s *Server) createDelegation(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	var req struct {
		AgentID string `json:"agent_id"`
		HumanID string `json:"human_id"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	switch {
	case !validUUID(req.AgentID) || !validUUID(req.HumanID):
		s.fail(w, r, badRequest("agent_id and human_id must be uuids"))
		return
	case strings.TrimSpace(req.Reason) == "":
		s.fail(w, r, badRequest("reason is required"))
		return
	}
	var d identity.Delegation
	err := s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		d, err = identity.Delegate(ctx, tx, p.Actor, p.Tenant, req.AgentID, req.HumanID, req.Reason, p.As.ID)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// endDelegation ends a delegation, once.
//
//	POST /v1/agent-delegations/{id}/end {reason}
func (s *Server) endDelegation(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	id := r.PathValue("id")
	if !validUUID(id) {
		s.fail(w, r, notFound("delegation"))
		return
	}
	reason, err := readReason(w, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var d identity.Delegation
	err = s.db.WithTenant(ctx, p.Tenant, p.As, func(tx pgx.Tx) (err error) {
		d, err = identity.EndDelegation(ctx, tx, p.Actor, p.Tenant, id, reason, p.As.ID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		err = notFound("delegation")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// readReason decodes {"reason": "..."}, which must not be blank.
func readReason(w http.ResponseWriter, r *http.Request) (string, error) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return "", badRequest("reason is required")
	}
	return req.Reason, nil
}

func loadPrincipal(r *http.Request, tx pgx.Tx, id string) (principalWithRoles, error) {
	pr, err := identity.ByID(r.Context(), tx, id)
	if err != nil {
		return principalWithRoles{}, err
	}
	roles, err := identity.ActiveGrants(r.Context(), tx, id)
	return principalWithRoles{Principal: pr, Roles: roles}, err
}
