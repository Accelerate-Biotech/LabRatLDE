-- 0009_role_gates: only scientists record lab work; only reviewers and QA sign (SPEC-006).
--
-- Authority checks live in the database (11.10(g)), so no endpoint, script
-- or later spec can skip them. Like every role check they bind lde_app; the
-- owner (migrations, maintenance) passes.

-- SPEC-006-R06
CREATE FUNCTION lab_records_require_scientist() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM lde_require_role('scientist');
    RETURN NULL;
END;
$$;

-- Statement-level, so a 5000-row reading batch costs one check.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'experiments', 'protocol_versions', 'lineage_nodes', 'lineage_edges', 'instruments', 'instrument_readings'
    ] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I
            FOR EACH STATEMENT EXECUTE FUNCTION lab_records_require_scientist()', t || '_role_gate', t);
    END LOOP;
END;
$$;

-- SPEC-006-R06: signing a run is a reviewer's or QA's act, not a scientist's.
CREATE FUNCTION runs_role_gate() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.state = 'signed' AND OLD.state <> 'signed' THEN
        PERFORM lde_require_role('reviewer', 'qa');
    ELSE
        PERFORM lde_require_role('scientist');
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER runs_role_gate
    BEFORE INSERT OR UPDATE OR DELETE ON runs
    FOR EACH ROW EXECUTE FUNCTION runs_role_gate();
