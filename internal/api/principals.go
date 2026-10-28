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
