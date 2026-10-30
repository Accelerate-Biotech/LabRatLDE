-- 0011_change_sets: change sets, the unit of every revision (SPEC-007).
--
-- A change set is proposed, then committed, rejected or withdrawn; the last
-- three are terminal. A direct edit inserts a change set already committed.
-- Only a human scientist commits or rejects; only the author replaces or
-- withdraws a proposal. These rules bind lde_app only: the owner runs
-- migrations and maintenance and sits outside RLS (owner_maintenance). The
-- database stamps source, author_id, decided_by and decided_at, so a client
-- can't claim another source or decider.

-- SPEC-007-R12
CREATE TABLE change_sets (
    id              uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id       uuid NOT NULL REFERENCES tenants (id),
    title           text NOT NULL CHECK (btrim(title) <> ''),
    reason          text NOT NULL CHECK (btrim(reason) <> ''),
    source          text NOT NULL DEFAULT 'human' CHECK (source IN ('human', 'parser', 'agent')),
    author          text NOT NULL,
    author_id       uuid,
    state           text NOT NULL CHECK (state IN ('proposed', 'committed', 'rejected', 'withdrawn')),
    items           jsonb NOT NULL CHECK (jsonb_typeof(items) = 'array'),
    items_sha256    bytea GENERATED ALWAYS AS (protocol_sha256(items::text)) STORED,
    decided_by      uuid,
    decided_at      timestamptz,
    decision_reason text CHECK (btrim(decision_reason) <> ''),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, author_id)  REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, decided_by) REFERENCES principals (tenant_id, id),
    CHECK ((state = 'proposed') = (decided_at IS NULL)),
    CHECK ((decided_at IS NULL) = (decided_by IS NULL)),
    CHECK ((state = 'proposed') = (decision_reason IS NULL))
);
CREATE INDEX change_sets_state ON change_sets (tenant_id, state, created_at);

-- SPEC-007-R06: raises LDE05 as lde_app unless the current principal is an
-- enabled human. A no-op for the owner.
CREATE FUNCTION lde_require_human() RETURNS void
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF current_user = 'lde_app' AND NOT EXISTS (
        SELECT 1 FROM principals pr
        WHERE pr.id = lde_current_principal() AND pr.kind = 'human' AND pr.enabled
    ) THEN
        RAISE EXCEPTION 'only a human may commit or reject a change set' USING ERRCODE = 'LDE05';
    END IF;
END;
$$;

GRANT EXECUTE ON FUNCTION lde_require_human() TO lde_app;

-- SPEC-007-R06, SPEC-007-R12
CREATE FUNCTION change_sets_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'change sets are never removed (TRUNCATE rejected)' USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'change set % is never removed (DELETE rejected)', OLD.id USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.state NOT IN ('proposed', 'committed') THEN
            RAISE EXCEPTION 'change set % cannot be created % (only proposed or committed)', NEW.id, NEW.state
                USING ERRCODE = 'check_violation';
        END IF;
    ELSE
        IF OLD.state <> 'proposed' THEN
            RAISE EXCEPTION 'change set % is % and cannot change', OLD.id, OLD.state USING ERRCODE = 'LDE03';
        END IF;
        IF (NEW.id, NEW.tenant_id, NEW.source, NEW.author, NEW.author_id, NEW.created_at)
           IS DISTINCT FROM
           (OLD.id, OLD.tenant_id, OLD.source, OLD.author, OLD.author_id, OLD.created_at) THEN
            RAISE EXCEPTION 'change set % (%): identity and authorship never change', OLD.id, OLD.state
                USING ERRCODE = 'LDE03';
        END IF;
        IF current_user = 'lde_app' AND NEW.state IN ('proposed', 'withdrawn')
           AND lde_current_principal() IS DISTINCT FROM OLD.author_id THEN
            RAISE EXCEPTION 'change set % (%): only its author may replace or withdraw it', OLD.id, OLD.state
                USING ERRCODE = 'LDE05';
        END IF;
        IF NEW.state <> 'proposed' AND (NEW.title, NEW.reason, NEW.items) IS DISTINCT FROM (OLD.title, OLD.reason, OLD.items) THEN
            RAISE EXCEPTION 'change set % (%): title, reason and items cannot change when it is decided', OLD.id, OLD.state
                USING ERRCODE = 'LDE03';
        END IF;
    END IF;
    PERFORM lde_require_role('scientist');
    IF NEW.state IN ('committed', 'rejected') THEN
        PERFORM lde_require_human();
    END IF;
    IF current_user = 'lde_app' THEN
        IF TG_OP = 'INSERT' THEN
            NEW.author_id := lde_current_principal();
            NEW.source := CASE (SELECT kind FROM principals WHERE id = NEW.author_id)
                WHEN 'agent' THEN 'agent'
                WHEN 'service' THEN 'parser'
                ELSE 'human'
            END;
            NEW.created_at := clock_timestamp();
        END IF;
        IF NEW.state <> 'proposed' THEN
            NEW.decided_by := lde_current_principal();
            NEW.decided_at := clock_timestamp();
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER change_sets_guard
    BEFORE INSERT OR UPDATE OR DELETE ON change_sets
    FOR EACH ROW EXECUTE FUNCTION change_sets_guard();
CREATE TRIGGER change_sets_no_truncate
    BEFORE TRUNCATE ON change_sets
    FOR EACH STATEMENT EXECUTE FUNCTION change_sets_guard();

-- SPEC-007-R06
ALTER TABLE change_sets ENABLE ROW LEVEL SECURITY;
ALTER TABLE change_sets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON change_sets TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON change_sets TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT, INSERT, UPDATE (title, reason, items, state, decided_by, decided_at, decision_reason)
    ON change_sets TO lde_app;
