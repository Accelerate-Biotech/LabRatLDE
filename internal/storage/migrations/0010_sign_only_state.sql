-- 0010_sign_only_state: signing a run changes its state and nothing else (SPEC-006).
--
-- 0009 let reviewer or QA through on any update into `signed`, so the same
-- statement could also rewrite params, times or links. Now any other changed
-- column needs scientist too, as every other change to a run does (R06).

-- SPEC-006-R06
CREATE OR REPLACE FUNCTION runs_role_gate() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.state = 'signed' AND OLD.state <> 'signed' THEN
        PERFORM lde_require_role('reviewer', 'qa');
        IF (NEW.id, NEW.tenant_id, NEW.experiment_id, NEW.protocol_version_id, NEW.node_id, NEW.params,
            NEW.started_at, NEW.finished_at, NEW.created_by, NEW.created_at)
           IS DISTINCT FROM
           (OLD.id, OLD.tenant_id, OLD.experiment_id, OLD.protocol_version_id, OLD.node_id, OLD.params,
            OLD.started_at, OLD.finished_at, OLD.created_by, OLD.created_at) THEN
            PERFORM lde_require_role('scientist');
        END IF;
    ELSE
        PERFORM lde_require_role('scientist');
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
