-- 0003_rls: tenant isolation via Row-Level Security.
--
-- The server connects as the schema owner but runs every request inside
-- storage.WithTenant, which does `SET LOCAL ROLE lde_app` and sets
-- app.tenant_id. lde_app is not the table owner, and every table is FORCE'd,
-- so policies apply unconditionally. SET ROLE sheds superuser for the
-- transaction, so this holds even in the dev compose stack; in production,
-- make the login role a NOSUPERUSER owner anyway, as defence in depth.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'lde_app') THEN
        CREATE ROLE lde_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
END;
$$;

-- Lets the login role `SET ROLE lde_app` even when it isn't a superuser.
DO $$
BEGIN
    EXECUTE format('GRANT lde_app TO %I', current_user);
END;
$$;

-- NULL (and therefore zero rows) when no tenant is set: fail closed.
CREATE FUNCTION lde_current_tenant() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

GRANT USAGE ON SCHEMA public TO lde_app;
GRANT EXECUTE ON FUNCTION lde_current_tenant(), audit_digest(uuid, bigint, timestamptz, text, text, text, text, text, text, bytea),
    audit_lp(text), audit_verify_chain(uuid) TO lde_app;

GRANT SELECT ON tenants TO lde_app;
GRANT SELECT, INSERT, UPDATE ON experiments, lineage_nodes, runs, instruments TO lde_app;
GRANT SELECT, INSERT ON lineage_edges, instrument_readings TO lde_app;
-- The ledger: read and append. Nothing else, ever.
GRANT SELECT, INSERT ON audit_events TO lde_app;
GRANT SELECT ON audit_chain_heads TO lde_app;

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_self ON tenants TO lde_app USING (id = lde_current_tenant());

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'experiments', 'lineage_nodes', 'lineage_edges', 'runs', 'instruments',
        'instrument_readings', 'audit_events', 'audit_chain_heads'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I TO lde_app
                USING (tenant_id = lde_current_tenant())
                WITH CHECK (tenant_id = lde_current_tenant())', t);
    END LOOP;
END;
$$;

-- FORCE RLS also binds the owner, which runs migrations and dev seeding
-- outside WithTenant. Give the owner an explicit escape hatch; lde_app never
-- matches it.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'tenants', 'experiments', 'lineage_nodes', 'lineage_edges', 'runs', 'instruments',
        'instrument_readings', 'audit_events', 'audit_chain_heads'
    ] LOOP
        EXECUTE format('CREATE POLICY owner_maintenance ON %I TO %I USING (true) WITH CHECK (true)', t, current_user);
    END LOOP;
END;
$$;
