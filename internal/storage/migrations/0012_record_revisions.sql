-- 0012_record_revisions: every revision of a record, written by trigger (SPEC-007).
--
-- A revision is a full snapshot of a record's revisable fields (label and
-- attrs of a node, params of a run, title of an experiment). Triggers write
-- it, so no role can edit a record without leaving one, and nobody can alter
-- or remove a revision, the owner included. After the first revision, a
-- change needs a change set committed in the same transaction (its xmin);
-- the revision takes its author and reason from that change set. lde_app
-- regains, column by column, the UPDATE that 0003 revoked, now behind the
-- trigger. Existing rows get revision 1 here, before the triggers exist.

-- SPEC-007-R01
CREATE TABLE record_revisions (
    tenant_id     uuid NOT NULL REFERENCES tenants (id),
    entity_type   text NOT NULL CHECK (entity_type IN ('lineage_node', 'run', 'experiment', 'note')),
    entity_id     uuid NOT NULL,
    rev           int NOT NULL CHECK (rev > 0),
    value         jsonb NOT NULL,
    author        text NOT NULL,
    reason        text NOT NULL,
    change_set_id uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, entity_type, entity_id, rev),
    FOREIGN KEY (tenant_id, change_set_id) REFERENCES change_sets (tenant_id, id)
);

-- SPEC-007-R01: revision 1 for every existing record, with no audit events;
-- the creation events already exist.
INSERT INTO record_revisions (tenant_id, entity_type, entity_id, rev, value, author, reason, change_set_id, created_at)
SELECT tenant_id, 'lineage_node', id, 1, jsonb_build_object('label', label, 'attrs', attrs), created_by, 'created', NULL, created_at
FROM lineage_nodes;
INSERT INTO record_revisions (tenant_id, entity_type, entity_id, rev, value, author, reason, change_set_id, created_at)
SELECT tenant_id, 'run', id, 1, jsonb_build_object('params', params), created_by, 'created', NULL, created_at
FROM runs;
INSERT INTO record_revisions (tenant_id, entity_type, entity_id, rev, value, author, reason, change_set_id, created_at)
SELECT tenant_id, 'experiment', id, 1, jsonb_build_object('title', title), created_by, 'created', NULL, created_at
FROM experiments;

-- SPEC-007-R01, SPEC-007-R02: arguments are the entity type, then the
-- revisable column names. SECURITY DEFINER, so lde_app needs no INSERT grant.
-- Two concurrent edits of one record may collide on the primary key; the
-- loser fails with a unique violation and rolls back.
-- Every relation is schema-qualified and pg_temp is searched last: otherwise
-- lde_app could create TEMP tables named change_sets or record_revisions that
-- shadow the real ones here and forge a committed change set.
CREATE FUNCTION record_revise() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp AS $$
DECLARE
    val     jsonb;
    cs      public.change_sets%ROWTYPE;
    setting text := current_setting('app.change_set_id', true);
BEGIN
    SELECT jsonb_object_agg(k, to_jsonb(NEW) -> k) INTO val FROM unnest(TG_ARGV[1:TG_NARGS - 1]) k;
    IF TG_OP = 'UPDATE' THEN
        IF val = (SELECT jsonb_object_agg(k, to_jsonb(OLD) -> k) FROM unnest(TG_ARGV[1:TG_NARGS - 1]) k) THEN
            RETURN NULL;
        END IF;
    END IF;
    IF setting IS NOT NULL AND setting <> '' THEN
        SELECT * INTO cs FROM public.change_sets c
        WHERE c.id = setting::uuid AND c.tenant_id = NEW.tenant_id AND c.state = 'committed'
          AND c.xmin = pg_current_xact_id()::xid;
    END IF;
    IF cs.id IS NULL AND (TG_OP = 'UPDATE' OR (setting IS NOT NULL AND setting <> '')) THEN
        RAISE EXCEPTION '% % changed outside a change set committed in this transaction', TG_ARGV[0], NEW.id
            USING ERRCODE = 'LDE03';
    END IF;
    INSERT INTO public.record_revisions (tenant_id, entity_type, entity_id, rev, value, author, reason, change_set_id, created_at)
    SELECT NEW.tenant_id, TG_ARGV[0], NEW.id, COALESCE(max(r.rev), 0) + 1, val,
           COALESCE(cs.author, NEW.created_by), COALESCE(cs.reason, 'created'), cs.id, clock_timestamp()
    FROM public.record_revisions r
    WHERE r.tenant_id = NEW.tenant_id AND r.entity_type = TG_ARGV[0] AND r.entity_id = NEW.id;
    RETURN NULL;
END;
$$;

CREATE TRIGGER lineage_nodes_revisions
    AFTER INSERT OR UPDATE ON lineage_nodes
    FOR EACH ROW EXECUTE FUNCTION record_revise('lineage_node', 'label', 'attrs');
CREATE TRIGGER runs_revisions
    AFTER INSERT OR UPDATE ON runs
    FOR EACH ROW EXECUTE FUNCTION record_revise('run', 'params');
CREATE TRIGGER experiments_revisions
    AFTER INSERT OR UPDATE ON experiments
    FOR EACH ROW EXECUTE FUNCTION record_revise('experiment', 'title');

-- SPEC-007-R03
CREATE FUNCTION record_revisions_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'revisions are append-only (% rejected)', TG_OP USING ERRCODE = 'LDE03';
END;
$$;

CREATE TRIGGER record_revisions_append_only
    BEFORE UPDATE OR DELETE ON record_revisions
    FOR EACH ROW EXECUTE FUNCTION record_revisions_reject_mutation();
CREATE TRIGGER record_revisions_no_truncate
    BEFORE TRUNCATE ON record_revisions
    FOR EACH STATEMENT EXECUTE FUNCTION record_revisions_reject_mutation();

-- SPEC-007-R10: runs_signed_immutable (0004) skips aborted runs; the params
-- of any run under a sealed node are frozen.
CREATE FUNCTION runs_params_sealed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF lineage_node_sealed(OLD.node_id) THEN
        RAISE EXCEPTION 'run % is sealed: its params cannot change', OLD.id USING ERRCODE = 'LDE04';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runs_params_sealed
    BEFORE UPDATE OF params ON runs
    FOR EACH ROW WHEN (OLD.params IS DISTINCT FROM NEW.params)
    EXECUTE FUNCTION runs_params_sealed();

-- SPEC-007-R03
ALTER TABLE record_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE record_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON record_revisions TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON record_revisions TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT ON record_revisions TO lde_app;

-- SPEC-007-R02: amends SPEC-002-R08; every other column stays ungranted.
GRANT UPDATE (label, attrs) ON lineage_nodes TO lde_app;
GRANT UPDATE (title) ON experiments TO lde_app;
