-- 0002_audit_ledger: the append-only, hash-chained audit trail (21 CFR 11.10(e)).
--
-- The database does not merely store the chain; it enforces it:
--   1. audit_digest() recomputes each event's SHA-256 with the exact v1
--      encoding used by internal/audit.Digest (see docs/audit-chain.md).
--   2. The BEFORE INSERT trigger rejects any event whose seq, prev_hash, or
--      hash doesn't extend the tenant's current head, or whose timestamp
--      didn't come from this database's clock.
--   3. UPDATE, DELETE and TRUNCATE are rejected by trigger and not granted.
--   4. audit_verify_chain() lets an auditor re-verify from plain SQL.

-- Canonical payload is stored as text: those exact bytes were hashed. jsonb
-- would normalise them and silently break verification. payload_json is a
-- derived, queryable copy.
CREATE TABLE audit_events (
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    seq          bigint NOT NULL CHECK (seq > 0),
    occurred_at  timestamptz NOT NULL,
    actor        text NOT NULL CHECK (actor <> ''),
    action       text NOT NULL CHECK (action <> ''),
    entity_type  text NOT NULL,
    entity_id    text NOT NULL,
    reason       text NOT NULL DEFAULT '',
    payload      text NOT NULL,
    payload_json jsonb GENERATED ALWAYS AS (payload::jsonb) STORED,
    prev_hash    bytea NOT NULL CHECK (octet_length(prev_hash) = 32),
    hash         bytea NOT NULL CHECK (octet_length(hash) = 32),
    PRIMARY KEY (tenant_id, seq),
    UNIQUE (hash)
);
CREATE INDEX audit_events_entity ON audit_events (tenant_id, entity_type, entity_id, seq);

-- Tip of each tenant's chain. Maintained only by the SECURITY DEFINER trigger.
CREATE TABLE audit_chain_heads (
    tenant_id uuid PRIMARY KEY REFERENCES tenants (id),
    seq       bigint NOT NULL,
    hash      bytea NOT NULL
);

-- lp(x) = u64be(len(utf8(x))) || utf8(x)
CREATE FUNCTION audit_lp(t text) RETURNS bytea
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT int8send(octet_length(convert_to(t, 'UTF8'))::bigint) || convert_to(t, 'UTF8')
$$;

CREATE FUNCTION audit_digest(
    p_tenant uuid, p_seq bigint, p_at timestamptz, p_actor text, p_action text,
    p_entity_type text, p_entity_id text, p_reason text, p_payload text, p_prev bytea
) RETURNS bytea
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT sha256(
        convert_to('LDE-AUDIT-v1', 'UTF8')
        || int8send(p_seq)
        || int8send((EXTRACT(epoch FROM p_at) * 1000000)::bigint)
        || audit_lp(p_tenant::text)
        || audit_lp(p_actor)
        || audit_lp(p_action)
        || audit_lp(p_entity_type)
        || audit_lp(p_entity_id)
        || audit_lp(p_reason)
        || audit_lp(p_payload)
        || p_prev
    )
$$;

CREATE FUNCTION audit_events_link() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
    head audit_chain_heads%ROWTYPE;
BEGIN
    SELECT * INTO head FROM audit_chain_heads WHERE tenant_id = NEW.tenant_id FOR UPDATE;
    IF NOT FOUND THEN
        head.seq  := 0;
        head.hash := decode(repeat('00', 32), 'hex');
    END IF;

    IF NEW.seq <> head.seq + 1 OR NEW.prev_hash <> head.hash THEN
        RAISE EXCEPTION 'audit event (tenant %, seq %) does not extend chain head %',
            NEW.tenant_id, NEW.seq, head.seq USING ERRCODE = 'LDE02';
    END IF;

    IF NEW.hash <> audit_digest(NEW.tenant_id, NEW.seq, NEW.occurred_at, NEW.actor, NEW.action,
                                NEW.entity_type, NEW.entity_id, NEW.reason, NEW.payload, NEW.prev_hash) THEN
        RAISE EXCEPTION 'audit event (tenant %, seq %) hash does not match content',
            NEW.tenant_id, NEW.seq USING ERRCODE = 'LDE02';
    END IF;

    -- Computer-generated timestamps (11.10(e)): must be "now" by our clock.
    IF NEW.occurred_at > clock_timestamp() OR NEW.occurred_at < clock_timestamp() - interval '1 minute' THEN
        RAISE EXCEPTION 'audit event timestamp % is not current', NEW.occurred_at USING ERRCODE = 'LDE02';
    END IF;

    INSERT INTO audit_chain_heads (tenant_id, seq, hash) VALUES (NEW.tenant_id, NEW.seq, NEW.hash)
    ON CONFLICT (tenant_id) DO UPDATE SET seq = EXCLUDED.seq, hash = EXCLUDED.hash;
    RETURN NEW;
END;
$$;

CREATE TRIGGER audit_events_link
    BEFORE INSERT ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_link();

CREATE FUNCTION audit_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit ledger is append-only (% rejected)', TG_OP USING ERRCODE = 'LDE03';
END;
$$;

CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_reject_mutation();
CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_reject_mutation();
CREATE TRIGGER audit_heads_append_only
    BEFORE DELETE ON audit_chain_heads
    FOR EACH ROW EXECUTE FUNCTION audit_reject_mutation();
CREATE TRIGGER audit_heads_no_truncate
    BEFORE TRUNCATE ON audit_chain_heads
    FOR EACH STATEMENT EXECUTE FUNCTION audit_reject_mutation();

-- Re-derive a tenant's chain in SQL. Returns the first broken seq (NULL when
-- intact). Runs with the caller's privileges, so RLS scopes it to one tenant.
CREATE FUNCTION audit_verify_chain(p_tenant uuid)
RETURNS TABLE (events bigint, head_seq bigint, head_hash text, broken_at bigint)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    ev        audit_events%ROWTYPE;
    prev_seq  bigint := 0;
    prev_hash bytea  := decode(repeat('00', 32), 'hex');
BEGIN
    events := 0;
    FOR ev IN SELECT * FROM audit_events WHERE tenant_id = p_tenant ORDER BY seq LOOP
        IF ev.seq <> prev_seq + 1 OR ev.prev_hash <> prev_hash
           OR ev.hash <> audit_digest(ev.tenant_id, ev.seq, ev.occurred_at, ev.actor, ev.action,
                                      ev.entity_type, ev.entity_id, ev.reason, ev.payload, ev.prev_hash) THEN
            broken_at := ev.seq;
            EXIT;
        END IF;
        events    := events + 1;
        prev_seq  := ev.seq;
        prev_hash := ev.hash;
    END LOOP;
    head_seq  := prev_seq;
    head_hash := encode(prev_hash, 'hex');
    RETURN NEXT;
END;
$$;
