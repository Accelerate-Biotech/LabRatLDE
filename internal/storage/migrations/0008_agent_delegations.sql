-- 0008_agent_delegations: agents act for a human, never beyond them (SPEC-006).
--
-- An admin delegates a human to an agent. While the delegation is active the
-- agent, acting for that human (app.on_behalf_of), holds only the roles both
-- of them hold. A delegation ends once and is never removed.

-- SPEC-006-R08
CREATE TABLE agent_delegations (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    agent_id   uuid NOT NULL,
    human_id   uuid NOT NULL,
    reason     text NOT NULL CHECK (btrim(reason) <> ''),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    ended_by   uuid,
    ended_at   timestamptz,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id)   REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, human_id)   REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by)   REFERENCES principals (tenant_id, id),
    CHECK ((ended_at IS NULL) = (ended_by IS NULL))
);
CREATE UNIQUE INDEX agent_delegations_active ON agent_delegations (tenant_id, agent_id, human_id)
    WHERE ended_at IS NULL;

-- SPEC-006-R08
CREATE FUNCTION agent_delegations_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'agent delegations are never removed (TRUNCATE rejected)' USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'agent delegation % is never removed (DELETE rejected)', OLD.id USING ERRCODE = 'LDE03';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.ended_at IS NOT NULL THEN
            RAISE EXCEPTION 'agent delegation % has already ended', OLD.id USING ERRCODE = 'LDE03';
        END IF;
        IF (NEW.id, NEW.tenant_id, NEW.agent_id, NEW.human_id, NEW.reason, NEW.created_by, NEW.created_at)
           IS DISTINCT FROM
           (OLD.id, OLD.tenant_id, OLD.agent_id, OLD.human_id, OLD.reason, OLD.created_by, OLD.created_at) THEN
            RAISE EXCEPTION 'agent delegation %: only the ending may change', OLD.id USING ERRCODE = 'LDE03';
        END IF;
    END IF;
    PERFORM lde_require_role('admin');
    IF TG_OP = 'INSERT' THEN
        IF NOT EXISTS (SELECT 1 FROM principals WHERE id = NEW.agent_id AND kind = 'agent') THEN
            RAISE EXCEPTION 'delegation agent % is not an agent principal', NEW.agent_id USING ERRCODE = 'check_violation';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM principals WHERE id = NEW.human_id AND kind = 'human') THEN
            RAISE EXCEPTION 'delegation human % is not a human principal', NEW.human_id USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF current_user = 'lde_app' THEN
        IF TG_OP = 'INSERT' THEN
            NEW.created_by := lde_current_principal();
            NEW.created_at := clock_timestamp();
        ELSE
            NEW.ended_by := lde_current_principal();
            NEW.ended_at := clock_timestamp();
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER agent_delegations_guard
    BEFORE INSERT OR UPDATE OR DELETE ON agent_delegations
    FOR EACH ROW EXECUTE FUNCTION agent_delegations_guard();
CREATE TRIGGER agent_delegations_no_truncate
    BEFORE TRUNCATE ON agent_delegations
    FOR EACH STATEMENT EXECUTE FUNCTION agent_delegations_guard();
CREATE CONSTRAINT TRIGGER agent_delegations_audited
    AFTER INSERT OR UPDATE ON agent_delegations DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION lde_require_audit('agent_delegation', 'delegation.created', 'delegation.ended');

-- SPEC-006-R08: an agent holds a role only while an active delegation from an
-- enabled human it acts for exists, and both of them hold the role.
CREATE OR REPLACE FUNCTION lde_has_role(r text, s text DEFAULT NULL) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT CASE (SELECT kind FROM principals WHERE id = lde_current_principal())
        WHEN 'agent' THEN
            lde_principal_holds(lde_current_principal(), r, s)
            AND lde_principal_holds(lde_on_behalf_of(), r, s)
            AND EXISTS (
                SELECT 1 FROM agent_delegations d
                JOIN principals h ON h.tenant_id = d.tenant_id AND h.id = d.human_id
                WHERE d.agent_id = lde_current_principal() AND d.human_id = lde_on_behalf_of()
                  AND d.ended_at IS NULL AND h.kind = 'human'
            )
        ELSE lde_principal_holds(lde_current_principal(), r, s)
    END
$$;

ALTER TABLE agent_delegations ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_delegations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_delegations TO lde_app
    USING (tenant_id = lde_current_tenant())
    WITH CHECK (tenant_id = lde_current_tenant());
DO $$
BEGIN
    EXECUTE format('CREATE POLICY owner_maintenance ON agent_delegations TO %I USING (true) WITH CHECK (true)', current_user);
END;
$$;
GRANT SELECT, INSERT, UPDATE (ended_by, ended_at) ON agent_delegations TO lde_app;
