-- 0005_protocol_versions: protocols become immutable, numbered versions (SPEC-003).
--
-- An experiment's protocol now lives in protocol_versions: append-only,
-- gapless per experiment, each with its own lineage node. A run pins the
-- version it executes, and the pin can't change. Existing experiments are
-- backfilled as version 1 with an audit event each, chained exactly as
-- audit.Append would. The old experiments.protocol_* columns are dropped.

-- SPEC-003-R09
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM runs) OR EXISTS (SELECT 1 FROM experiments WHERE protocol_md = '') THEN
        RAISE EXCEPTION 'SPEC-003 migration needs a database with no runs and no empty protocols; reset it with: docker compose down -v';
    END IF;
END;
$$;

-- SPEC-003-R01
CREATE FUNCTION protocol_sha256(t text) RETURNS bytea
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT sha256(convert_to(t, 'UTF8'))
$$;

GRANT EXECUTE ON FUNCTION protocol_sha256(text) TO lde_app;

-- SPEC-003-R01
CREATE TABLE protocol_versions (
    id            uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenants (id),
    experiment_id uuid NOT NULL,
    version       int NOT NULL CHECK (version > 0),
    protocol_md   text NOT NULL CHECK (protocol_md <> ''),
    protocol_meta jsonb NOT NULL DEFAULT '{}',
    sha256        bytea GENERATED ALWAYS AS (protocol_sha256(protocol_md)) STORED,
    node_id       uuid NOT NULL,
    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, experiment_id, id),
    UNIQUE (tenant_id, experiment_id, version),
    UNIQUE (tenant_id, node_id),
    FOREIGN KEY (tenant_id, experiment_id) REFERENCES experiments (tenant_id, id),
    FOREIGN KEY (tenant_id, node_id)       REFERENCES lineage_nodes (tenant_id, id)
);
CREATE INDEX protocol_versions_meta_gin ON protocol_versions USING gin (protocol_meta jsonb_path_ops);

-- SPEC-003-R02
CREATE FUNCTION protocol_versions_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'protocol versions are immutable (TRUNCATE rejected)' USING ERRCODE = 'LDE03';
    END IF;
    RAISE EXCEPTION 'protocol version % is immutable (% rejected)', OLD.id, TG_OP USING ERRCODE = 'LDE03';
END;
$$;

CREATE TRIGGER protocol_versions_append_only
    BEFORE UPDATE OR DELETE ON protocol_versions
    FOR EACH ROW EXECUTE FUNCTION protocol_versions_reject_mutation();
CREATE TRIGGER protocol_versions_no_truncate
    BEFORE TRUNCATE ON protocol_versions
    FOR EACH STATEMENT EXECUTE FUNCTION protocol_versions_reject_mutation();

-- SPEC-003-R03
CREATE FUNCTION protocol_versions_gapless() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.version <> COALESCE((SELECT max(version) FROM protocol_versions
                                WHERE tenant_id = NEW.tenant_id AND experiment_id = NEW.experiment_id), 0) + 1 THEN
        RAISE EXCEPTION 'protocol version % of experiment % is not the next version', NEW.version, NEW.experiment_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER protocol_versions_gapless
    BEFORE INSERT ON protocol_versions
    FOR EACH ROW EXECUTE FUNCTION protocol_versions_gapless();

-- SPEC-003-R02
ALTER TABLE protocol_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE protocol_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON protocol_versions TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON protocol_versions TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT, INSERT ON protocol_versions TO lde_app;

-- SPEC-003-R09
DO $$
DECLARE
    e       experiments%ROWTYPE;
    node    uuid;
    pv      protocol_versions%ROWTYPE;
    head    audit_chain_heads%ROWTYPE;
    at      timestamptz;
    payload text;
    ev_seq  bigint;
    prev    bytea;
BEGIN
    FOR e IN SELECT * FROM experiments ORDER BY tenant_id, created_at, id LOOP
        PERFORM pg_advisory_xact_lock(hashtextextended('lde.audit:' || e.tenant_id::text, 0));

        INSERT INTO lineage_nodes (tenant_id, kind, label, created_by)
        VALUES (e.tenant_id, 'protocol', e.slug || ' v1', e.created_by)
        RETURNING id INTO node;

        INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, protocol_meta, node_id, created_by, created_at)
        VALUES (e.tenant_id, e.id, 1, e.protocol_md, e.protocol_meta, node, e.created_by, e.created_at)
        RETURNING * INTO pv;

        SELECT * INTO head FROM audit_chain_heads WHERE tenant_id = e.tenant_id;
        IF FOUND THEN
            ev_seq := head.seq + 1;
            prev   := head.hash;
        ELSE
            ev_seq := 1;
            prev   := decode(repeat('00', 32), 'hex');
        END IF;

        at := clock_timestamp();
        payload := format('{"experiment_id":"%s","node_id":"%s","sha256":"%s","version":1}',
                          e.id, node, encode(pv.sha256, 'hex'));

        INSERT INTO audit_events
            (tenant_id, seq, occurred_at, actor, action, entity_type, entity_id, reason, payload, prev_hash, hash)
        VALUES
            (e.tenant_id, ev_seq, at, 'system:migration-0005', 'protocol.version_created', 'protocol_version',
             pv.id::text, 'SPEC-003 backfill', payload, prev,
             audit_digest(e.tenant_id, ev_seq, at, 'system:migration-0005', 'protocol.version_created',
                          'protocol_version', pv.id::text, 'SPEC-003 backfill', payload, prev));
    END LOOP;
END;
$$;

-- SPEC-003-R04
ALTER TABLE runs ADD COLUMN protocol_version_id uuid NOT NULL;
ALTER TABLE runs ADD FOREIGN KEY (tenant_id, experiment_id, protocol_version_id)
    REFERENCES protocol_versions (tenant_id, experiment_id, id);

-- SPEC-003-R11
CREATE FUNCTION runs_reject_repin() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'run % protocol version is pinned', OLD.id USING ERRCODE = 'LDE03';
END;
$$;

CREATE TRIGGER runs_protocol_pinned
    BEFORE UPDATE OF protocol_version_id ON runs
    FOR EACH ROW WHEN (OLD.protocol_version_id IS DISTINCT FROM NEW.protocol_version_id)
    EXECUTE FUNCTION runs_reject_repin();

-- SPEC-003-R09
DROP INDEX experiments_meta_gin;
ALTER TABLE experiments DROP COLUMN protocol_md, DROP COLUMN protocol_meta;
