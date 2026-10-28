package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/identity"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

// Principal is the authenticated caller. Every audit event records Actor.
type Principal struct {
	Tenant string
	Actor  string            // the handle, or "<human> via <agent>"
	As     storage.Principal // who WithTenant acts as
	Handle string
	Kind   string
	// ForHandle is the human an agent acts for; "" otherwise.
	ForHandle string
}

type principalKey struct{}

func principal(ctx context.Context) Principal { return ctx.Value(principalKey{}).(Principal) }

func unauthorized(msg string) error { return &httpError{http.StatusUnauthorized, msg} }
func forbidden(msg string) error    { return &httpError{http.StatusForbidden, msg} }

var errNoPrincipal = forbidden("no enabled principal for this identity in the tenant")

// identify resolves the caller to a principal (SPEC-006).
//
// In prod, only a bearer JWT from the configured issuer counts, and the tenant
// comes from its claims (11.10(d)). In dev, X-LDE-Tenant and X-LDE-Actor are
// trusted, and an unknown actor is created with every tenant role.
func (s *Server) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			p   Principal
			err error
		)
		if s.cfg.Env == "prod" {
			p, err = s.fromToken(r)
		} else {
			w.Header().Set("X-LDE-Auth", "dev")
			p, err = s.fromDevHeaders(r)
		}
		if err != nil {
			var he *httpError
			if s.cfg.Env == "prod" && errors.As(err, &he) && he.status == http.StatusUnauthorized {
				w.Header().Set("WWW-Authenticate", `Bearer realm="labrat"`)
			}
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// fromToken verifies the bearer token, then finds its issuer and subject in
// the tenant its claim names. It reads no tenant data until the token checks
// out.
func (s *Server) fromToken(r *http.Request) (Principal, error) {
	if s.verifier == nil {
		return Principal{}, &httpError{http.StatusNotImplemented, "OIDC verifier not configured"}
	}
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		return Principal{}, unauthorized("bearer token required")
	}
	tok, err := s.verifier.Verify(r.Context(), raw)
	if err != nil {
		s.log.Info("bearer token rejected", "err", err)
		return Principal{}, unauthorized("invalid bearer token")
	}
	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return Principal{}, unauthorized("invalid bearer token claims")
	}
	tenant, _ := claims[s.cfg.OIDCTenantClaim].(string)
	tenant = strings.ToLower(tenant)
	if !validUUID(tenant) {
		return Principal{}, unauthorized("token has no valid " + s.cfg.OIDCTenantClaim + " claim")
	}
	return s.resolve(r, tenant, func(ctx context.Context, tx pgx.Tx) (identity.Principal, error) {
		return identity.BySubject(ctx, tx, tok.Issuer, tok.Subject)
	})
}

// fromDevHeaders trusts the dev headers. A new handle becomes a human with
// every tenant role, created as the owner and audited as system:dev-auth.
func (s *Server) fromDevHeaders(r *http.Request) (Principal, error) {
	tenant := strings.ToLower(r.Header.Get("X-LDE-Tenant"))
	handle := strings.ToLower(strings.TrimSpace(r.Header.Get("X-LDE-Actor")))
	if !validUUID(tenant) || handle == "" {
		return Principal{}, unauthorized("X-LDE-Tenant (uuid) and X-LDE-Actor headers are required")
	}
	if !identity.ValidHandle(handle) {
		return Principal{}, unauthorized("X-LDE-Actor must be a handle: lowercase letters, digits and ._@+-")
	}
	byHandle := func(ctx context.Context, tx pgx.Tx) (identity.Principal, error) {
		return identity.ByHandle(ctx, tx, handle)
	}
	p, err := s.resolve(r, tenant, byHandle)
	if !errors.Is(err, errNoPrincipal) {
		return p, err
	}
	if err := s.createDevPrincipal(r.Context(), tenant, handle); err != nil {
		return Principal{}, err
	}
	return s.resolve(r, tenant, byHandle)
}

func (s *Server) createDevPrincipal(ctx context.Context, tenant, handle string) error {
	const actor, reason = "system:dev-auth", "dev sign-in"
	err := s.db.WithOwner(ctx, func(tx pgx.Tx) error {
		p, err := identity.Create(ctx, tx, actor, reason, identity.Principal{
			TenantID: tenant, Kind: "human", Handle: handle, Issuer: "dev", Subject: handle,
		})
		if err != nil {
			return err
		}
		for _, role := range identity.TenantRoles {
			if _, err := identity.Grant(ctx, tx, actor, tenant, p.ID, role, "", reason, p.ID); err != nil {
				return err
			}
		}
		return nil
	})
	switch {
	case storage.IsBadReference(err):
		return unauthorized("unknown tenant")
	case storage.ErrorCode(err) == "23505":
		return nil // a concurrent request created it first
	}
	return err
}

// resolve looks the caller up in tenant, acting as nobody (reads only). An
// agent must name, in X-LDE-On-Behalf-Of, an enabled human who has delegated
// to it; it then acts as "<human> via <agent>" (R07, R09).
func (s *Server) resolve(r *http.Request, tenant string, lookup func(context.Context, pgx.Tx) (identity.Principal, error)) (Principal, error) {
	ctx := r.Context()
	onBehalfOf := strings.ToLower(strings.TrimSpace(r.Header.Get("X-LDE-On-Behalf-Of")))
	var who, human identity.Principal
	err := s.db.WithTenant(ctx, tenant, storage.Principal{}, func(tx pgx.Tx) (err error) {
		if who, err = lookup(ctx, tx); err != nil {
			return err
		}
		switch {
		case !who.Enabled:
			return forbidden("principal " + who.Handle + " is disabled")
		case who.Kind != "agent" && onBehalfOf != "":
			return badRequest("X-LDE-On-Behalf-Of is only for agent principals")
		case who.Kind != "agent":
			return nil
		case onBehalfOf == "":
			return forbidden("agent " + who.Handle + " must name the human it acts for in X-LDE-On-Behalf-Of")
		}
		human, err = identity.ByHandle(ctx, tx, onBehalfOf)
		noDelegation := forbidden("agent " + who.Handle + " has no active delegation from " + onBehalfOf)
		if errors.Is(err, pgx.ErrNoRows) {
			return noDelegation
		} else if err != nil {
			return err
		}
		ok, err := identity.ActiveDelegation(ctx, tx, who.ID, human.ID)
		if err == nil && !ok {
			err = noDelegation
		}
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Principal{}, errNoPrincipal
	case err != nil:
		return Principal{}, err
	}
	p := Principal{
		Tenant: tenant, Actor: who.Handle, As: storage.Principal{ID: who.ID}, Handle: who.Handle, Kind: who.Kind,
	}
	if who.Kind == "agent" {
		p.Actor = human.Handle + " via " + who.Handle
		p.As.OnBehalfOf = human.ID
		p.ForHandle = human.Handle
	}
	return p, nil
}
