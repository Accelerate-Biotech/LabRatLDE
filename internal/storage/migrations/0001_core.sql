-- 0001_core: tenants, experiments, the lineage DAG, runs, and instrument data.
--
-- Conventions:
--   * Every tenant-owned table carries tenant_id and a UNIQUE (tenant_id, id).
--     Foreign keys are composite (tenant_id, x_id), so a row can never point
--     at another tenant's row, even though FK checks bypass RLS.
--   * Requires PostgreSQL >= 14 (gen_random_uuid, numeric EXTRACT(epoch)).

CREATE TABLE tenants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Experiments are protocols-as-code: Markdown with YAML frontmatter, stored
-- verbatim. protocol_meta is the parsed frontmatter, indexed for querying.
CREATE TABLE experiments (
    id            uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenants (id),
    slug          text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    title         text NOT NULL,
    protocol_md   text NOT NULL DEFAULT '',
    protocol_meta jsonb NOT NULL DEFAULT '{}',
    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, slug)
);
CREATE INDEX experiments_meta_gin ON experiments USING gin (protocol_meta jsonb_path_ops);

-- ---------------------------------------------------------------------------
-- Lineage DAG (W3C PROV). Edges point downstream: from = antecedent,
-- to = consequent; the triple reads "to <relation> from".
-- ---------------------------------------------------------------------------
CREATE TABLE lineage_nodes (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    kind       text NOT NULL CHECK (kind IN ('sample', 'reagent', 'protocol', 'run', 'instrument', 'dataset', 'analysis')),
    label      text NOT NULL,
    attrs      jsonb NOT NULL DEFAULT '{}',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
CREATE INDEX lineage_nodes_kind ON lineage_nodes (tenant_id, kind);

CREATE TABLE lineage_edges (
    tenant_id  uuid NOT NULL,
    from_id    uuid NOT NULL,
    to_id      uuid NOT NULL,
    relation   text NOT NULL CHECK (relation IN ('used', 'wasGeneratedBy', 'wasDerivedFrom', 'wasInformedBy', 'wasAssociatedWith', 'wasAttributedTo')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, from_id, to_id, relation),
    FOREIGN KEY (tenant_id, from_id) REFERENCES lineage_nodes (tenant_id, id),
    FOREIGN KEY (tenant_id, to_id)   REFERENCES lineage_nodes (tenant_id, id),
    CHECK (from_id <> to_id)
);
CREATE INDEX lineage_edges_to ON lineage_edges (tenant_id, to_id);

-- Acyclicity is a database invariant, not an application convention.
CREATE FUNCTION lineage_reject_cycle() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Serialise edge writes per tenant so two concurrent inserts can't each
    -- pass the check and jointly close a loop.
    PERFORM pg_advisory_xact_lock(hashtextextended('lde.lineage:' || NEW.tenant_id::text, 0));
    IF EXISTS (
        WITH RECURSIVE downstream (id) AS (
            SELECT NEW.to_id
            UNION
            SELECT e.to_id
            FROM lineage_edges e
            JOIN downstream d ON e.tenant_id = NEW.tenant_id AND e.from_id = d.id
        )
        SELECT 1 FROM downstream WHERE id = NEW.from_id
    ) THEN
        RAISE EXCEPTION 'lineage edge % -> % would create a cycle', NEW.from_id, NEW.to_id
            USING ERRCODE = 'LDE01';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER lineage_edges_acyclic
    BEFORE INSERT ON lineage_edges
    FOR EACH ROW EXECUTE FUNCTION lineage_reject_cycle();

-- ---------------------------------------------------------------------------
-- Runs: one execution of an experiment. Each run is also a lineage node.
-- ---------------------------------------------------------------------------
CREATE TABLE runs (
    id            uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id     uuid NOT NULL,
    experiment_id uuid NOT NULL,
    node_id       uuid NOT NULL,
    state         text NOT NULL DEFAULT 'planned'
                  CHECK (state IN ('planned', 'running', 'paused', 'completed', 'failed', 'aborted', 'signed')),
    params        jsonb NOT NULL DEFAULT '{}',
    started_at    timestamptz,
    finished_at   timestamptz,
    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, node_id),
    FOREIGN KEY (tenant_id, experiment_id) REFERENCES experiments (tenant_id, id),
    FOREIGN KEY (tenant_id, node_id)       REFERENCES lineage_nodes (tenant_id, id)
);
CREATE INDEX runs_experiment ON runs (tenant_id, experiment_id);

-- Signed runs are sealed. Corrections are new runs linked via wasDerivedFrom.
CREATE FUNCTION runs_reject_signed_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.state = 'signed' THEN
        RAISE EXCEPTION 'run % is signed and immutable', OLD.id USING ERRCODE = 'LDE03';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER runs_signed_immutable
    BEFORE UPDATE OR DELETE ON runs
    FOR EACH ROW EXECUTE FUNCTION runs_reject_signed_mutation();

-- ---------------------------------------------------------------------------
-- Instruments and their streamed readings.
-- ---------------------------------------------------------------------------
CREATE TABLE instruments (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    node_id    uuid NOT NULL,
    name       text NOT NULL,
    kind       text NOT NULL, -- free-form: hplc, qpcr, plate_reader, liquid_handler, ...
    attrs      jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name),
    FOREIGN KEY (tenant_id, node_id) REFERENCES lineage_nodes (tenant_id, id)
);

-- Narrow, append-mostly fact table. Each streamed upload becomes a `dataset`
-- lineage node (dataset_id). For sustained high-rate instruments, partition
-- by month on ts and roll cold partitions out to Parquet in object storage.
CREATE TABLE instrument_readings (
    tenant_id     uuid NOT NULL,
    instrument_id uuid NOT NULL,
    dataset_id    uuid NOT NULL,
    ts            timestamptz NOT NULL,
    channel       text NOT NULL,
    value         double precision NOT NULL,
    unit          text NOT NULL DEFAULT '',
    FOREIGN KEY (tenant_id, instrument_id) REFERENCES instruments (tenant_id, id),
    FOREIGN KEY (tenant_id, dataset_id)    REFERENCES lineage_nodes (tenant_id, id)
);
CREATE INDEX instrument_readings_series ON instrument_readings (tenant_id, instrument_id, ts);
CREATE INDEX instrument_readings_dataset ON instrument_readings (tenant_id, dataset_id);
