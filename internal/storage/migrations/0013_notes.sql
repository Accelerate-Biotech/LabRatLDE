-- 0013_notes: free-text notes on lineage nodes (SPEC-007).
--
-- A note belongs to a lineage node. A note written before the node's run is
-- signed is an ordinary record: its body may be edited under a change set
-- until the seal, after which it is frozen. A note written once the node is
-- sealed is an annotation: it never alters the sealed record and may still be
-- corrected under a change set. Notes touch only this table, so adding one
-- never trips the sealed-edge or sealed-node triggers. Every body is a
-- revision (record_revise, 0012).

-- SPEC-007-R13
CREATE TABLE notes (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    node_id    uuid NOT NULL,
    body       text NOT NULL CHECK (btrim(body) <> ''),
    annotation boolean NOT NULL DEFAULT false,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, node_id) REFERENCES lineage_nodes (tenant_id, id)
);
CREATE INDEX notes_node ON notes (tenant_id, node_id, created_at);

-- SPEC-007-R13: INSERT decides annotation from the seal; UPDATE may change
-- only body, and only while the note is an annotation or the node unsealed.
-- The pinned search_path (also applied to nested calls) and public. names stop
-- a TEMP table named runs or lineage_edges shadowing the real ones.
CREATE FUNCTION notes_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public, pg_temp AS $$
DECLARE
    tid  uuid;
    node uuid;
BEGIN
    IF TG_OP = 'TRUNCATE' OR TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'notes cannot be deleted (% rejected)', TG_OP USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'INSERT' THEN
        tid := NEW.tenant_id;
        node := NEW.node_id;
    ELSE
        tid := OLD.tenant_id;
        node := OLD.node_id;
    END IF;

    -- Lock every run whose seal covers this node, so a concurrent sign waits
    -- for this transaction (or this check sees the committed sign).
    PERFORM 1 FROM public.runs r
    WHERE r.tenant_id = tid
      AND (r.node_id = node
           OR r.node_id IN (SELECT e.from_id FROM public.lineage_edges e
                            WHERE e.tenant_id = tid AND e.to_id = node
                              AND e.relation = 'wasGeneratedBy'))
    FOR SHARE OF r;

    IF TG_OP = 'INSERT' THEN
        NEW.annotation := public.lineage_node_sealed(NEW.node_id);
        RETURN NEW;
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.node_id IS DISTINCT FROM OLD.node_id OR NEW.annotation IS DISTINCT FROM OLD.annotation
       OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'only the body of note % can change', OLD.id USING ERRCODE = 'LDE03';
    END IF;
    IF NOT OLD.annotation AND public.lineage_node_sealed(OLD.node_id) THEN
        RAISE EXCEPTION 'note % was written before its node was sealed and is frozen', OLD.id
            USING ERRCODE = 'LDE04';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER notes_guard
    BEFORE INSERT OR UPDATE OR DELETE ON notes
    FOR EACH ROW EXECUTE FUNCTION notes_reject_mutation();
CREATE TRIGGER notes_no_truncate
    BEFORE TRUNCATE ON notes
    FOR EACH STATEMENT EXECUTE FUNCTION notes_reject_mutation();

-- SPEC-007-R13, SPEC-007-R01
CREATE TRIGGER notes_revisions
    AFTER INSERT OR UPDATE ON notes
    FOR EACH ROW EXECUTE FUNCTION record_revise('note', 'body');

-- SPEC-007-R13: like every lab record, notes are written by scientists.
CREATE TRIGGER notes_role_gate
    BEFORE INSERT OR UPDATE OR DELETE ON notes
    FOR EACH STATEMENT EXECUTE FUNCTION lab_records_require_scientist();

-- SPEC-007-R13
ALTER TABLE notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE notes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notes TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON notes TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT, INSERT, UPDATE (body) ON notes TO lde_app;
