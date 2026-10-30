-- 0007_role_grants: roles, the admin gate, and audited identity changes (SPEC-006).
--
-- A role grant is append-only except for its revocation, which happens once.
-- Role checks bind lde_app only: the owner runs migrations, bootstrap-admin
-- and dev sign-in, and already sits outside RLS (owner_maintenance). Every
-- change to principals and role_grants must be audited in its own
-- transaction, which a deferred constraint trigger proves at commit. That
-- check binds the owner too.

-- SPEC-006-R05
CREATE TABLE role_grants (
    id            uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenants (id),
    principal_id  uuid NOT NULL,
    role          text NOT NULL CHECK (role IN ('scientist', 'reviewer', 'qa', 'admin', 'schema_steward')),
    scope         text,
    reason        text NOT NULL CHECK (btrim(reason) <> ''),
    granted_by    uuid NOT NULL,
    granted_at    timestamptz NOT NULL DEFAULT now(),
    revoked_by    uuid,
    revoked_at    timestamptz,
    revoke_reason text CHECK (btrim(revoke_reason) <> ''),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, granted_by)   REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, revoked_by)   REFERENCES principals (tenant_id, id),
    -- Only the schema steward role is scoped (to a schema namespace, SPEC-010).
    CHECK ((role = 'schema_steward') = (scope IS NOT NULL)),
    CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND (revoked_at IS NULL) = (revoke_reason IS NULL))
);
CREATE UNIQUE INDEX role_grants_active ON role_grants (tenant_id, principal_id, role, COALESCE(scope, ''))
    WHERE revoked_at IS NULL;

-- Whether principal p is enabled and holds an active grant of r with scope s.
CREATE FUNCTION lde_principal_holds(p uuid, r text, s text) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM principals pr
        JOIN role_grants g ON g.tenant_id = pr.tenant_id AND g.principal_id = pr.id
        WHERE pr.id = p AND pr.enabled
          AND g.revoked_at IS NULL AND g.role = r AND g.scope IS NOT DISTINCT FROM s
    )
$$;

-- Whether the current principal holds r (with scope s). Agents hold nothing
-- here; 0008 replaces this with the delegation rule.
CREATE FUNCTION lde_has_role(r text, s text DEFAULT NULL) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT lde_principal_holds(lde_current_principal(), r, s)
       AND (SELECT kind FROM principals WHERE id = lde_current_principal()) <> 'agent'
$$;

-- Raises LDE05 unless the current principal holds one of roles. A no-op for
-- the owner.
CREATE FUNCTION lde_require_role(VARIADIC roles text[]) RETURNS void
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF current_user = 'lde_app' AND NOT EXISTS (SELECT 1 FROM unnest(roles) r WHERE lde_has_role(r)) THEN
        RAISE EXCEPTION 'role denied: needs %', array_to_string(roles, ' or ') USING ERRCODE = 'LDE05';
    END IF;
END;
$$;

GRANT EXECUTE ON FUNCTION lde_principal_holds(uuid, text, text), lde_has_role(text, text),
    lde_require_role(text[]) TO lde_app;

-- SPEC-006-R05
CREATE FUNCTION role_grants_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'role grants are never removed (TRUNCATE rejected)' USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'role grant % is never removed (DELETE rejected)', OLD.id USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.revoked_at IS NOT NULL THEN
            RAISE EXCEPTION 'role grant % is already revoked', OLD.id USING ERRCODE = 'LDE03';
        END IF;
        IF (NEW.id, NEW.tenant_id, NEW.principal_id, NEW.role, NEW.scope, NEW.reason, NEW.granted_by, NEW.granted_at)
           IS DISTINCT FROM
           (OLD.id, OLD.tenant_id, OLD.principal_id, OLD.role, OLD.scope, OLD.reason, OLD.granted_by, OLD.granted_at) THEN
            RAISE EXCEPTION 'role grant %: only the revocation may change', OLD.id USING ERRCODE = 'LDE03';
        END IF;
    END IF;
    PERFORM lde_require_role('admin');
    IF current_user = 'lde_app' THEN
        IF TG_OP = 'INSERT' THEN
            NEW.granted_by := lde_current_principal();
            NEW.granted_at := clock_timestamp();
        ELSE
            NEW.revoked_by := lde_current_principal();
            NEW.revoked_at := clock_timestamp();
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER role_grants_guard
    BEFORE INSERT OR UPDATE OR DELETE ON role_grants
    FOR EACH ROW EXECUTE FUNCTION role_grants_guard();
CREATE TRIGGER role_grants_no_truncate
    BEFORE TRUNCATE ON role_grants
    FOR EACH STATEMENT EXECUTE FUNCTION role_grants_guard();

-- Admins provision principals.
CREATE FUNCTION principals_admin_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM lde_require_role('admin');
    RETURN NEW;
END;
$$;

CREATE TRIGGER principals_admin_only
    BEFORE INSERT OR UPDATE ON principals
    FOR EACH ROW EXECUTE FUNCTION principals_admin_only();

-- SPEC-006-R05: the change and its audit event commit together. Arguments:
-- entity type, the action an INSERT needs, then the actions an UPDATE may
-- carry. The event must have been written by this transaction (its xmin).
CREATE FUNCTION lde_require_audit() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    actions text[];
BEGIN
    IF TG_OP = 'INSERT' THEN
        actions := ARRAY[TG_ARGV[1]];
    ELSE
        actions := TG_ARGV[2:TG_NARGS - 1];
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM audit_events a
        WHERE a.tenant_id = NEW.tenant_id AND a.entity_type = TG_ARGV[0] AND a.entity_id = NEW.id::text
          AND a.action = ANY (actions) AND a.xmin = pg_current_xact_id()::xid
    ) THEN
        RAISE EXCEPTION '% % changed without a % audit event in its transaction',
            TG_ARGV[0], NEW.id, array_to_string(actions, ' or ') USING ERRCODE = 'LDE02';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER principals_audited
    AFTER INSERT ON principals DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION lde_require_audit('principal', 'principal.created');
CREATE CONSTRAINT TRIGGER principals_enabled_audited
    AFTER UPDATE ON principals DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (OLD.enabled IS DISTINCT FROM NEW.enabled)
    EXECUTE FUNCTION lde_require_audit('principal', '', 'principal.enabled', 'principal.disabled');
CREATE CONSTRAINT TRIGGER role_grants_audited
    AFTER INSERT OR UPDATE ON role_grants DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION lde_require_audit('role_grant', 'role.granted', 'role.revoked');

ALTER TABLE role_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON role_grants TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON role_grants TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT, INSERT, UPDATE (revoked_by, revoked_at, revoke_reason) ON role_grants TO lde_app;
