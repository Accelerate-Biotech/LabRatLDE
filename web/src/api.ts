// The only module that calls fetch. Types mirror the JSON the Go handlers in
// internal/api emit; optional fields are the ones with omitempty.

const TENANT_KEY = 'labrat.tenant';
const ACTOR_KEY = 'labrat.actor';

export const DEV_TENANT = '00000000-0000-4000-8000-000000000001';

export type Page<T> = { items: T[]; next_cursor: string | null };

export type Meta = { env: 'dev' | 'prod' };

export type ExperimentSummary = { id: string; slug: string; title: string; created_at: string };

export type Experiment = {
  id: string;
  slug: string;
  title: string;
  protocol_version_id: string;
  protocol_version: number;
  protocol_sha256: string;
  protocol_md: string;
  protocol_meta: unknown;
  created_by: string;
  created_at: string;
};

export type ProtocolVersion = {
  id: string;
  experiment_id: string;
  version: number;
  sha256: string;
  node_id: string;
  created_by: string;
  created_at: string;
  protocol_md?: string;
  protocol_meta?: unknown;
};

export type RunState = 'planned' | 'running' | 'paused' | 'completed' | 'failed' | 'aborted' | 'signed';

export type RunSummary = { id: string; label: string; state: RunState; created_at: string };

export type Run = {
  id: string;
  tenant_id: string;
  experiment_id: string;
  protocol_version_id: string;
  node_id: string;
  state: RunState;
  params: unknown;
  started_at?: string;
  finished_at?: string;
  created_by: string;
  created_at: string;
};

export type RunInput = { node_id: string; kind: NodeKind; label: string };

export type RunDataset = { node_id: string; label: string; count: number | null; sha256: string | null };

export type RunDetail = Run & {
  protocol_version: number;
  protocol_sha256: string;
  inputs: RunInput[];
  datasets: RunDataset[];
};

export type Instrument = {
  id: string;
  node_id: string;
  name: string;
  kind: string;
  attrs: Record<string, unknown>;
  created_at: string;
};

export type NodeKind = 'sample' | 'reagent' | 'protocol' | 'run' | 'instrument' | 'dataset' | 'analysis';

export type Relation = 'used' | 'wasGeneratedBy' | 'wasDerivedFrom' | 'wasInformedBy' | 'wasAssociatedWith' | 'wasAttributedTo';

export type LineageNode = {
  id: string;
  kind: NodeKind;
  label: string;
  attrs?: Record<string, unknown>;
  created_at: string;
};

export type LineageEdge = { from: string; to: string; relation: Relation };

// edges is null when the subgraph has no edges (Go nil slice).
export type Lineage = { root: string; nodes: LineageNode[]; edges: LineageEdge[] | null };

export type LineageDirection = 'upstream' | 'downstream' | 'both';

export type AuditEvent = {
  tenant_id: string;
  seq: number;
  occurred_at: string;
  actor: string;
  action: string;
  entity_type: string;
  entity_id: string;
  reason?: string;
  payload: unknown;
  prev_hash: string;
  hash: string;
};

export type AuditReport = {
  valid: boolean;
  events: number;
  head: { seq: number; hash: string };
  break?: { seq: number; reason: string };
};

export type AuditVerify = {
  valid: boolean;
  engine: AuditReport;
  database: { events: number; head_seq: number; head_hash: string; broken_at?: number };
  agree: boolean;
};

// stored reads a session key, tolerating storage that is disabled or throws.
function stored(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function storedActor(): string | null {
  return stored(ACTOR_KEY);
}

export function setSession(tenant: string, actor: string): void {
  try {
    localStorage.setItem(TENANT_KEY, tenant);
    localStorage.setItem(ACTOR_KEY, actor);
  } catch {
    // Storage unavailable: the header-less request will fail with a clear error.
  }
}

export function clearSession(): void {
  try {
    localStorage.removeItem(TENANT_KEY);
    localStorage.removeItem(ACTOR_KEY);
  } catch {
    // Nothing stored, nothing to clear.
  }
}

async function get<T>(path: string): Promise<T> {
  const headers: Record<string, string> = {};
  const tenant = stored(TENANT_KEY);
  const actor = stored(ACTOR_KEY);
  if (tenant) headers['X-LDE-Tenant'] = tenant;
  if (actor) headers['X-LDE-Actor'] = actor;

  const res = await fetch(path, { headers });
  if (!res.ok) {
    let msg: string | undefined;
    try {
      const body = await res.json();
      msg = body?.error;
    } catch {
      // Not JSON: fall back to the status code.
    }
    throw new Error(msg ?? String(res.status));
  }
  return res.json() as Promise<T>;
}

// qs builds a query string from the defined, non-empty params.
function qs(params: Record<string, string | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v) q.set(k, v);
  }
  const s = q.toString();
  return s ? `?${s}` : '';
}

export function meta(): Promise<Meta> {
  return get('/v1/meta');
}

export function listExperiments(cursor?: string): Promise<Page<ExperimentSummary>> {
  return get(`/v1/experiments${qs({ cursor })}`);
}

export function getExperiment(id: string): Promise<Experiment> {
  return get(`/v1/experiments/${encodeURIComponent(id)}`);
}

export function listProtocolVersions(experimentId: string): Promise<ProtocolVersion[]> {
  return get(`/v1/experiments/${encodeURIComponent(experimentId)}/protocol-versions`);
}

export function getProtocolVersion(id: string): Promise<ProtocolVersion> {
  return get(`/v1/protocol-versions/${encodeURIComponent(id)}`);
}

export function listRuns(experimentId: string, cursor?: string): Promise<Page<RunSummary>> {
  return get(`/v1/experiments/${encodeURIComponent(experimentId)}/runs${qs({ cursor })}`);
}

export function getRun(id: string): Promise<RunDetail> {
  return get(`/v1/runs/${encodeURIComponent(id)}`);
}

export function listInstruments(cursor?: string): Promise<Page<Instrument>> {
  return get(`/v1/instruments${qs({ cursor })}`);
}

export function getLineage(id: string, direction: LineageDirection): Promise<Lineage> {
  return get(`/v1/lineage/${encodeURIComponent(id)}${qs({ direction })}`);
}

export function listAuditEvents(after: number, entityType?: string, entityId?: string): Promise<AuditEvent[]> {
  const filter = entityType && entityId ? { entity_type: entityType, entity_id: entityId } : {};
  return get(`/v1/audit/events${qs({ after: String(after), limit: '100', ...filter })}`);
}

export function verifyAudit(): Promise<AuditVerify> {
  return get('/v1/audit/verify');
}
