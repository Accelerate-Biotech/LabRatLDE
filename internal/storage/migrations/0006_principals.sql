-- 0006_principals: who can act in a tenant (SPEC-006).
--
-- A principal is a human, a service or an agent, bound to one identity at an
-- OIDC issuer. Handles are unique per tenant and, because principals are never
-- deleted or renamed, never reused (11.100(a), 11.300(a)). Only `enabled` may
-- change. Role grants (0007) and agent delegations (0008) hang off this table.

-- SPEC-006-R04
CREATE TABLE principals (
    id           uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    kind         text NOT NULL CHECK (kind IN ('human', 'service', 'agent')),
    -- Lowercase and spaceless, so "<human> via <agent>" actors are unambiguous.
    handle       text NOT NULL CHECK (handle ~ '^[a-z0-9][a-z0-9._@+-]{0,127}$'),
    display_name text NOT NULL DEFAULT '',
    email        text NOT NULL DEFAULT '',
    issuer       text NOT NULL CHECK (issuer <> ''),
    subject      text NOT NULL CHECK (subject <> ''),
    enabled      boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, handle),
    UNIQUE (tenant_id, issuer, subject)
);

-- The principal a tenant transaction acts as, and the human an agent acts
-- for. storage.WithTenant sets both; NULL when unset, so checks fail closed.
CREATE FUNCTION lde_current_principal() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT NULLIF(current_setting('app.principal_id', true), '')::uuid
$$;

CREATE FUNCTION lde_on_behalf_of() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT NULLIF(current_setting('app.on_behalf_of', true), '')::uuid
$$;

GRANT EXECUTE ON FUNCTION lde_current_principal(), lde_on_behalf_of() TO lde_app;

-- SPEC-006-R04
CREATE FUNCTION principals_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'principals are never removed (TRUNCATE rejected)' USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'principal % is never removed (DELETE rejected)', OLD.handle USING ERRCODE = 'LDE03';
    END IF;
    IF (NEW.id, NEW.tenant_id, NEW.kind, NEW.handle, NEW.display_name, NEW.email, NEW.issuer, NEW.subject, NEW.created_at)
       IS DISTINCT FROM
       (OLD.id, OLD.tenant_id, OLD.kind, OLD.handle, OLD.display_name, OLD.email, OLD.issuer, OLD.subject, OLD.created_at) THEN
        RAISE EXCEPTION 'principal %: only enabled may change', OLD.handle USING ERRCODE = 'LDE03';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER principals_immutable
    BEFORE UPDATE OR DELETE ON principals
    FOR EACH ROW EXECUTE FUNCTION principals_reject_mutation();
CREATE TRIGGER principals_no_truncate
    BEFORE TRUNCATE ON principals
    FOR EACH STATEMENT EXECUTE FUNCTION principals_reject_mutation();

ALTER TABLE principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE principals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON principals TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON principals TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT, INSERT, UPDATE (enabled) ON principals TO lde_app;
