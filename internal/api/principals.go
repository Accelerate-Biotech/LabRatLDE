package api

import (
	"net/http"

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

func loadPrincipal(r *http.Request, tx pgx.Tx, id string) (principalWithRoles, error) {
	pr, err := identity.ByID(r.Context(), tx, id)
	if err != nil {
		return principalWithRoles{}, err
	}
	roles, err := identity.ActiveGrants(r.Context(), tx, id)
	return principalWithRoles{Principal: pr, Roles: roles}, err
}
