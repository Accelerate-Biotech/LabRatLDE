-- 0004_sealing: a signed run seals what it signed (SPEC-002).
--
-- The sealed set of a run is its own lineage node plus every dataset node it
-- generated (wasGeneratedBy). Once sealed, those nodes can't gain inputs or
-- outputs, can't be changed or deleted, and their datasets can't gain
-- readings. Later work may still *use* sealed outputs: edges leading out of
-- a sealed node are fine unless they claim the node generated something new.
--
-- Aborted runs seal immediately (SPEC-002 open question 1); failed runs seal
-- once signed.

-- SPEC-002-R01
CREATE FUNCTION lineage_node_sealed(node uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM runs r
        WHERE r.node_id = node AND r.state IN ('signed', 'aborted')
    ) OR EXISTS (
        SELECT 1 FROM lineage_edges e
        JOIN runs r ON r.tenant_id = e.tenant_id AND r.node_id = e.from_id
        WHERE e.to_id = node AND e.relation = 'wasGeneratedBy'
          AND r.state IN ('signed', 'aborted')
    )
$$;

GRANT EXECUTE ON FUNCTION lineage_node_sealed(uuid) TO lde_app;

-- SPEC-002-R02, R03, R04
CREATE FUNCTION lineage_edges_reject_sealed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Lock every run whose seal this edge depends on, so a concurrent sign
    -- waits for this transaction (or this check sees the committed sign).
    PERFORM 1 FROM runs r
    WHERE r.tenant_id = NEW.tenant_id
      AND (r.node_id = NEW.to_id
           OR (NEW.relation = 'wasGeneratedBy' AND r.node_id = NEW.from_id)
           OR r.node_id IN (SELECT e.from_id FROM lineage_edges e
                            WHERE e.tenant_id = NEW.tenant_id AND e.to_id = NEW.to_id
                              AND e.relation = 'wasGeneratedBy'))
    FOR SHARE OF r;

    IF lineage_node_sealed(NEW.to_id) THEN
        RAISE EXCEPTION 'lineage node % is sealed and cannot gain inputs', NEW.to_id
            USING ERRCODE = 'LDE04';
    END IF;
    IF NEW.relation = 'wasGeneratedBy' AND lineage_node_sealed(NEW.from_id) THEN
        RAISE EXCEPTION 'lineage node % is sealed and cannot generate new outputs', NEW.from_id
            USING ERRCODE = 'LDE04';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER lineage_edges_sealed
    BEFORE INSERT ON lineage_edges
    FOR EACH ROW EXECUTE FUNCTION lineage_edges_reject_sealed();

-- SPEC-002-R05
CREATE FUNCTION lineage_nodes_reject_sealed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF lineage_node_sealed(OLD.id) THEN
        RAISE EXCEPTION '% lineage node % is sealed (% rejected)', OLD.kind, OLD.id, TG_OP
            USING ERRCODE = 'LDE04';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER lineage_nodes_sealed
    BEFORE UPDATE OR DELETE ON lineage_nodes
    FOR EACH ROW EXECUTE FUNCTION lineage_nodes_reject_sealed();

-- SPEC-002-R06, R07
--
-- Readings for a dataset may only be written by the transaction that created
-- the dataset node: its row's xmin must be the current transaction id. That
-- fails closed if the node was created inside a savepoint (xmin is then the
-- subtransaction's id); ingest never does that.
--
-- Statement-level with a transition table, so a 5000-row ingest batch costs
-- one check per dataset rather than one per reading.
CREATE FUNCTION instrument_readings_reject_sealed() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    ds uuid;
BEGIN
    FOR ds IN SELECT DISTINCT dataset_id FROM new_readings LOOP
        IF lineage_node_sealed(ds) THEN
            RAISE EXCEPTION 'dataset % is sealed and cannot gain readings', ds
                USING ERRCODE = 'LDE04';
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM lineage_nodes n
            WHERE n.id = ds AND n.xmin = pg_current_xact_id()::xid
        ) THEN
            RAISE EXCEPTION 'dataset % was created by another transaction and cannot gain readings', ds
                USING ERRCODE = 'LDE04';
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE TRIGGER instrument_readings_sealed
    AFTER INSERT ON instrument_readings
    REFERENCING NEW TABLE AS new_readings
    FOR EACH STATEMENT EXECUTE FUNCTION instrument_readings_reject_sealed();

-- SPEC-002-R08: no endpoint updates these tables, so nothing may. SPEC-007
-- re-grants column-level UPDATE behind revision triggers.
REVOKE UPDATE ON experiments, lineage_nodes, instruments FROM lde_app;
