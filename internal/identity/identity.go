// Package identity writes principals, role grants and agent delegations, each
// with its audit event in the caller's transaction (SPEC-006). The database
// decides who may: as lde_app, every write here needs admin, and the grantor,
// revoker and their times are stamped by triggers from the acting principal.
// As the owner (bootstrap, dev sign-in) the by-arguments are stored as given.
package identity

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
)

// TenantRoles are the roles that apply tenant-wide. schema_steward is scoped
// to a schema namespace (SPEC-010).
var TenantRoles = []string{"scientist", "reviewer", "qa", "admin"}

var handleRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{0,127}$`)

// ValidHandle reports whether h matches the principals.handle CHECK.
func ValidHandle(h string) bool { return handleRE.MatchString(h) }

type Principal struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Kind        string    `json:"kind"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	Issuer      string    `json:"issuer"`
	Subject     string    `json:"subject"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

type RoleGrant struct {
	ID           string     `json:"id"`
	PrincipalID  string     `json:"principal_id"`
	Role         string     `json:"role"`
	Scope        *string    `json:"scope"`
	Reason       string     `json:"reason"`
	GrantedBy    string     `json:"granted_by"`
	GrantedAt    time.Time  `json:"granted_at"`
	RevokedBy    *string    `json:"revoked_by"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
}

type Delegation struct {
	ID        string     `json:"id"`
	AgentID   string     `json:"agent_id"`
	HumanID   string     `json:"human_id"`
	Reason    string     `json:"reason"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	EndedBy   *string    `json:"ended_by"`
	EndedAt   *time.Time `json:"ended_at"`
}

const (
	principalColumns  = `id, tenant_id::text, kind, handle, display_name, email, issuer, subject, enabled, created_at`
	grantColumns      = `id, principal_id, role, scope, reason, granted_by, granted_at, revoked_by, revoked_at, revoke_reason`
	delegationColumns = `id, agent_id, human_id, reason, created_by, created_at, ended_by, ended_at`
)

func scanPrincipal(row pgx.Row) (Principal, error) {
	var p Principal
	err := row.Scan(&p.ID, &p.TenantID, &p.Kind, &p.Handle, &p.DisplayName, &p.Email, &p.Issuer, &p.Subject,
		&p.Enabled, &p.CreatedAt)
	return p, err
}

func scanGrant(row pgx.Row) (RoleGrant, error) {
	var g RoleGrant
	err := row.Scan(&g.ID, &g.PrincipalID, &g.Role, &g.Scope, &g.Reason, &g.GrantedBy, &g.GrantedAt,
		&g.RevokedBy, &g.RevokedAt, &g.RevokeReason)
	return g, err
}

func scanDelegation(row pgx.Row) (Delegation, error) {
	var d Delegation
	err := row.Scan(&d.ID, &d.AgentID, &d.HumanID, &d.Reason, &d.CreatedBy, &d.CreatedAt, &d.EndedBy, &d.EndedAt)
	return d, err
}

// Create inserts p into p.TenantID and audits principal.created.
func Create(ctx context.Context, tx pgx.Tx, actor, reason string, p Principal) (Principal, error) {
	out, err := scanPrincipal(tx.QueryRow(ctx, `
		INSERT INTO principals (tenant_id, kind, handle, display_name, email, issuer, subject)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+principalColumns,
		p.TenantID, p.Kind, p.Handle, p.DisplayName, p.Email, p.Issuer, p.Subject))
	if err != nil {
		return Principal{}, err
	}
	_, err = audit.Append(ctx, tx, out.TenantID, audit.Entry{
		Actor: actor, Action: "principal.created", EntityType: "principal", EntityID: out.ID, Reason: reason,
		Payload: map[string]any{"kind": out.Kind, "handle": out.Handle, "display_name": out.DisplayName,
			"email": out.Email, "issuer": out.Issuer, "subject": out.Subject},
	})
	return out, err
}

// SetEnabled enables or disables a principal and audits principal.enabled or
// principal.disabled. A principal already in that state is returned as is,
// with no event.
func SetEnabled(ctx context.Context, tx pgx.Tx, actor, tenantID, id string, enabled bool, reason string) (Principal, error) {
	p, err := scanPrincipal(tx.QueryRow(ctx, `UPDATE principals SET enabled = $2 WHERE id = $1 AND enabled <> $2
		RETURNING `+principalColumns, id, enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalColumns+` FROM principals WHERE id = $1`, id))
	} else if err != nil {
		return Principal{}, err
	}
	action := "principal.disabled"
	if enabled {
		action = "principal.enabled"
	}
	_, err = audit.Append(ctx, tx, tenantID, audit.Entry{
		Actor: actor, Action: action, EntityType: "principal", EntityID: id, Reason: reason,
		Payload: map[string]any{"handle": p.Handle},
	})
	return p, err
}

// ByHandle and BySubject return pgx.ErrNoRows when there is no such principal.
func ByHandle(ctx context.Context, tx pgx.Tx, handle string) (Principal, error) {
	return scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalColumns+` FROM principals WHERE handle = $1`, handle))
}

func BySubject(ctx context.Context, tx pgx.Tx, issuer, subject string) (Principal, error) {
	return scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalColumns+` FROM principals WHERE issuer = $1 AND subject = $2`,
		issuer, subject))
}

// Grant gives principalID role (scope "" is tenant-wide) and audits
// role.granted.
func Grant(ctx context.Context, tx pgx.Tx, actor, tenantID, principalID, role, scope, reason, grantedBy string) (RoleGrant, error) {
	g, err := scanGrant(tx.QueryRow(ctx, `
		INSERT INTO role_grants (tenant_id, principal_id, role, scope, reason, granted_by)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)
		RETURNING `+grantColumns,
		tenantID, principalID, role, scope, reason, grantedBy))
	if err != nil {
		return RoleGrant{}, err
	}
	_, err = audit.Append(ctx, tx, tenantID, audit.Entry{
		Actor: actor, Action: "role.granted", EntityType: "role_grant", EntityID: g.ID, Reason: reason,
		Payload: map[string]any{"principal_id": principalID, "role": role, "scope": g.Scope},
	})
	return g, err
}

// Revoke ends a grant and audits role.revoked. A grant already revoked fails
// with LDE03; an unknown one with pgx.ErrNoRows.
func Revoke(ctx context.Context, tx pgx.Tx, actor, tenantID, grantID, reason, revokedBy string) (RoleGrant, error) {
	g, err := scanGrant(tx.QueryRow(ctx, `
		UPDATE role_grants SET revoked_by = $2, revoked_at = now(), revoke_reason = $3 WHERE id = $1
		RETURNING `+grantColumns, grantID, revokedBy, reason))
	if err != nil {
		return RoleGrant{}, err
	}
	_, err = audit.Append(ctx, tx, tenantID, audit.Entry{
		Actor: actor, Action: "role.revoked", EntityType: "role_grant", EntityID: g.ID, Reason: reason,
		Payload: map[string]any{"principal_id": g.PrincipalID, "role": g.Role, "scope": g.Scope},
	})
	return g, err
}

// ActiveGrants lists a principal's unrevoked grants, oldest first.
func ActiveGrants(ctx context.Context, tx pgx.Tx, principalID string) ([]RoleGrant, error) {
	rows, err := tx.Query(ctx, `SELECT `+grantColumns+` FROM role_grants
		WHERE principal_id = $1 AND revoked_at IS NULL ORDER BY granted_at, id`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []RoleGrant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

// Delegate lets agentID act for humanID and audits delegation.created.
func Delegate(ctx context.Context, tx pgx.Tx, actor, tenantID, agentID, humanID, reason, createdBy string) (Delegation, error) {
	d, err := scanDelegation(tx.QueryRow(ctx, `
		INSERT INTO agent_delegations (tenant_id, agent_id, human_id, reason, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+delegationColumns,
		tenantID, agentID, humanID, reason, createdBy))
	if err != nil {
		return Delegation{}, err
	}
	_, err = audit.Append(ctx, tx, tenantID, audit.Entry{
		Actor: actor, Action: "delegation.created", EntityType: "agent_delegation", EntityID: d.ID, Reason: reason,
		Payload: map[string]any{"agent_id": agentID, "human_id": humanID},
	})
	return d, err
}

// EndDelegation ends a delegation and audits delegation.ended. One already
// ended fails with LDE03; an unknown one with pgx.ErrNoRows.
func EndDelegation(ctx context.Context, tx pgx.Tx, actor, tenantID, id, reason, endedBy string) (Delegation, error) {
	d, err := scanDelegation(tx.QueryRow(ctx, `
		UPDATE agent_delegations SET ended_by = $2, ended_at = now() WHERE id = $1
		RETURNING `+delegationColumns, id, endedBy))
	if err != nil {
		return Delegation{}, err
	}
	_, err = audit.Append(ctx, tx, tenantID, audit.Entry{
		Actor: actor, Action: "delegation.ended", EntityType: "agent_delegation", EntityID: d.ID, Reason: reason,
		Payload: map[string]any{"agent_id": d.AgentID, "human_id": d.HumanID},
	})
	return d, err
}

// ActiveDelegation reports whether agentID may act for humanID: an active
// delegation exists and the human is an enabled human.
func ActiveDelegation(ctx context.Context, tx pgx.Tx, agentID, humanID string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM agent_delegations d JOIN principals h ON h.id = d.human_id
		WHERE d.agent_id = $1 AND d.human_id = $2 AND d.ended_at IS NULL AND h.kind = 'human' AND h.enabled)`,
		agentID, humanID).Scan(&ok)
	return ok, err
}
