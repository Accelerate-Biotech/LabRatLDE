// The only module that calls fetch. Types mirror the JSON the Go handlers in
// internal/api emit; optional fields are the ones with omitempty.

const TENANT_KEY = 'labrat.tenant';
const ACTOR_KEY = 'labrat.actor';

export const DEV_TENANT = '00000000-0000-4000-8000-000000000001';

export type Page<T> = { items: T[]; next_cursor: string | null };

export type OIDCSettings = { issuer: string; client_id: string; scopes: string };

export type Meta = { env: 'dev' | 'prod'; idle_timeout_seconds: number; oidc?: OIDCSettings };

export type PrincipalKind = 'human' | 'service' | 'agent';

export type Role = 'scientist' | 'reviewer' | 'qa' | 'admin' | 'schema_steward';

export const TENANT_ROLES: Role[] = ['scientist', 'reviewer', 'qa', 'admin'];

export type RoleGrant = {
  id: string;
  principal_id: string;
  role: Role;
  scope: string | null;
  reason: string;
  granted_by: string;
  granted_at: string;
  revoked_by: string | null;
  revoked_at: string | null;
  revoke_reason: string | null;
};

export type Principal = {
  id: string;
  tenant_id: string;
  kind: PrincipalKind;
  handle: string;
  display_name: string;
  email: string;
  issuer: string;
  subject: string;
  enabled: boolean;
  created_at: string;
  roles: RoleGrant[];
};

export type Me = Principal & { actor: string; on_behalf_of: string | null; auth: 'dev' | 'oidc' };

export type Delegation = {
  id: string;
  agent_id: string;
  human_id: string;
  agent_handle: string;
  human_handle: string;
  reason: string;
  created_by: string;
  created_at: string;
};

export type NewPrincipal = {
  kind: PrincipalKind;
  handle: string;
  display_name: string;
  email: string;
  issuer: string;
  subject: string;
  reason: string;
};

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

// The OIDC access token lives in memory only (SPEC-006 design §3): a reload
// signs in again.
let accessToken: string | null = null;
let onUnauthorized: () => void = () => {};

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

// setUnauthorizedHandler registers what to do when the server rejects the
// credentials (an expired token): App returns to sign-in.
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`;
  } else {
    const tenant = stored(TENANT_KEY);
    const actor = stored(ACTOR_KEY);
    if (tenant) headers['X-LDE-Tenant'] = tenant;
    if (actor) headers['X-LDE-Actor'] = actor;
  }
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  if (res.status === 401 && path !== '/v1/meta') onUnauthorized();
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

function get<T>(path: string): Promise<T> {
  return request<T>('GET', path);
}

function post<T>(path: string, body: unknown): Promise<T> {
  return request<T>('POST', path, body);
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

export function getMe(): Promise<Me> {
  return get('/v1/me');
}

export function listPrincipals(cursor?: string): Promise<Page<Principal>> {
  return get(`/v1/principals${qs({ cursor, limit: '200' })}`);
}

export function createPrincipal(p: NewPrincipal): Promise<Principal> {
  return post('/v1/principals', p);
}

export function disablePrincipal(id: string, reason: string): Promise<Principal> {
  return post(`/v1/principals/${encodeURIComponent(id)}/disable`, { reason });
}

export function grantRole(principalId: string, role: Role, reason: string, scope = ''): Promise<RoleGrant> {
  return post('/v1/role-grants', { principal_id: principalId, role, scope, reason });
}

export function revokeRole(grantId: string, reason: string): Promise<RoleGrant> {
  return post(`/v1/role-grants/${encodeURIComponent(grantId)}/revoke`, { reason });
}

export function listDelegations(): Promise<{ items: Delegation[] }> {
  return get('/v1/agent-delegations');
}

export function createDelegation(agentId: string, humanId: string, reason: string): Promise<Delegation> {
  return post('/v1/agent-delegations', { agent_id: agentId, human_id: humanId, reason });
}

export function endDelegation(id: string, reason: string): Promise<Delegation> {
  return post(`/v1/agent-delegations/${encodeURIComponent(id)}/end`, { reason });
}
