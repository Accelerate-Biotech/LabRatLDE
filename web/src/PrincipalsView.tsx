import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import {
  createPrincipal,
  disablePrincipal,
  grantRole,
  listPrincipals,
  revokeRole,
  TENANT_ROLES,
  type NewPrincipal,
  type Principal,
  type PrincipalKind,
  type Role,
  type RoleGrant,
} from './api';

// Feedback after an action: what happened, or what went wrong.
export type Outcome = { ok: boolean; text: string } | null;

export function OutcomeLine({ outcome }: { outcome: Outcome }) {
  if (!outcome) return null;
  return outcome.ok ? <p role="status">{outcome.text}</p> : <p role="alert" className="error">{outcome.text}</p>;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// ReasonForm asks for the reason every audited change carries (11.10(e)).
export function ReasonForm({ title, submitLabel, onSubmit, onCancel, children }: {
  title: string;
  submitLabel: string;
  onSubmit: (reason: string) => Promise<void>;
  onCancel: () => void;
  children?: ReactNode;
}) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!reason.trim()) return;
    setBusy(true);
    setError(null);
    onSubmit(reason.trim()).catch((err: unknown) => {
      setBusy(false);
      setError(message(err));
    });
  }

  return (
    <form className="action-form" onSubmit={submit} aria-label={title}>
      <h2>{title}</h2>
      {children}
      <label htmlFor="action-reason">Reason (recorded in the audit trail)</label>
      <input id="action-reason" value={reason} onChange={(e) => setReason(e.target.value)} required autoFocus />
      {error && <p role="alert" className="error">{error}</p>}
      <div className="actions">
        <button type="submit" disabled={busy || !reason.trim()}>
          {submitLabel}
        </button>
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

type Action =
  | { type: 'create' }
  | { type: 'grant'; principal: Principal }
  | { type: 'revoke'; principal: Principal; grant: RoleGrant }
  | { type: 'disable'; principal: Principal };

// PrincipalsView is the admin screen for people, services and agents, and
// their roles. The database decides who may change what; a non-admin sees
// its refusal.
export default function PrincipalsView() {
  const [principals, setPrincipals] = useState<Principal[] | null>(null);
  const [more, setMore] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [action, setAction] = useState<Action | null>(null);
  const [outcome, setOutcome] = useState<Outcome>(null);

  const reload = useCallback(() => {
    listPrincipals()
      .then((p) => {
        setPrincipals(p.items);
        setMore(p.next_cursor !== null);
      })
      .catch((err: unknown) => setLoadError(message(err)));
  }, []);

  useEffect(reload, [reload]);

  // done closes the form, reports and reloads.
  function done(text: string) {
    setAction(null);
    setOutcome({ ok: true, text });
    reload();
  }

  function start(a: Action) {
    setOutcome(null);
    setAction(a);
  }

  if (loadError) return <p role="alert" className="error">{loadError}</p>;
  if (!principals) return <p className="muted">Loading principals…</p>;

  return (
    <article className="view">
      <h1>Principals</h1>
      <p className="muted">People, services and agents in this tenant. Handles are never reused; disable a principal instead of removing it.</p>
      <p>
        <button type="button" onClick={() => start({ type: 'create' })}>
          New principal
        </button>
      </p>
      <OutcomeLine outcome={outcome} />
      {action && <ActionPanel action={action} onDone={done} onCancel={() => setAction(null)} />}

      <table className="data">
        <caption className="sr-only">Principals and their roles</caption>
        <thead>
          <tr>
            <th scope="col">Handle</th>
            <th scope="col">Name</th>
            <th scope="col">Kind</th>
            <th scope="col">Status</th>
            <th scope="col">Roles</th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {principals.map((p) => (
            <tr key={p.id}>
              <th scope="row" className="mono">
                {p.handle}
              </th>
              <td>{p.display_name}</td>
              <td>{p.kind}</td>
              <td>{p.enabled ? 'Enabled' : 'Disabled'}</td>
              <td>
                {p.roles.length === 0 && <span className="muted">None</span>}
                <ul className="roles">
                  {p.roles.map((g) => (
                    <li key={g.id}>
                      {g.role}
                      {g.scope ? ` (${g.scope})` : ''}{' '}
                      <button type="button" aria-label={`Revoke ${g.role} from ${p.handle}`} onClick={() => start({ type: 'revoke', principal: p, grant: g })}>
                        Revoke
                      </button>
                    </li>
                  ))}
                </ul>
              </td>
              <td>
                {p.enabled && (
                  <div className="actions">
                    <button type="button" aria-label={`Grant a role to ${p.handle}`} onClick={() => start({ type: 'grant', principal: p })}>
                      Grant role
                    </button>
                    <button type="button" aria-label={`Disable ${p.handle}`} onClick={() => start({ type: 'disable', principal: p })}>
                      Disable
                    </button>
                  </div>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {more && <p className="muted">Showing the first 200 principals.</p>}
    </article>
  );
}

function ActionPanel({ action, onDone, onCancel }: { action: Action; onDone: (text: string) => void; onCancel: () => void }) {
  switch (action.type) {
    case 'create':
      return <CreateForm onDone={onDone} onCancel={onCancel} />;
    case 'grant':
      return <GrantForm principal={action.principal} onDone={onDone} onCancel={onCancel} />;
    case 'revoke': {
      const { principal: p, grant: g } = action;
      return (
        <ReasonForm
          title={`Revoke ${g.role} from ${p.handle}`}
          submitLabel="Revoke"
          onCancel={onCancel}
          onSubmit={(reason) => revokeRole(g.id, reason).then(() => onDone(`Revoked ${g.role} from ${p.handle}.`))}
        />
      );
    }
    case 'disable': {
      const p = action.principal;
      return (
        <ReasonForm
          title={`Disable ${p.handle}`}
          submitLabel="Disable"
          onCancel={onCancel}
          onSubmit={(reason) => disablePrincipal(p.id, reason).then(() => onDone(`Disabled ${p.handle}. They can no longer sign in.`))}
        >
          <p className="muted">Their records stay attributed to them, and the handle stays taken.</p>
        </ReasonForm>
      );
    }
  }
}

function GrantForm({ principal, onDone, onCancel }: { principal: Principal; onDone: (text: string) => void; onCancel: () => void }) {
  const held = new Set(principal.roles.map((g) => g.role));
  const choices = TENANT_ROLES.filter((r) => !held.has(r));
  const [role, setRole] = useState<Role | ''>(choices[0] ?? '');
  if (choices.length === 0) {
    return (
      <div className="action-form">
        <p>{principal.handle} already holds every tenant role.</p>
        <button type="button" onClick={onCancel}>
          Close
        </button>
      </div>
    );
  }
  return (
    <ReasonForm
      title={`Grant a role to ${principal.handle}`}
      submitLabel="Grant"
      onCancel={onCancel}
      onSubmit={(reason) => grantRole(principal.id, role as Role, reason).then(() => onDone(`Granted ${role} to ${principal.handle}.`))}
    >
      <label htmlFor="grant-role">Role</label>
      <select id="grant-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
        {choices.map((r) => (
          <option key={r} value={r}>
            {r}
          </option>
        ))}
      </select>
    </ReasonForm>
  );
}

function CreateForm({ onDone, onCancel }: { onDone: (text: string) => void; onCancel: () => void }) {
  const [p, setP] = useState<Omit<NewPrincipal, 'reason'>>({ kind: 'human', handle: '', display_name: '', email: '', issuer: '', subject: '' });
  const set = (k: keyof typeof p) => (e: { target: { value: string } }) => setP({ ...p, [k]: e.target.value });
  const field = (k: 'handle' | 'display_name' | 'email' | 'issuer' | 'subject', label: string, hint?: string) => (
    <>
      <label htmlFor={`new-${k}`}>{label}</label>
      <input id={`new-${k}`} value={p[k]} onChange={set(k)} required={k === 'handle' || k === 'issuer' || k === 'subject'} aria-describedby={hint ? `new-${k}-hint` : undefined} />
      {hint && (
        <span id={`new-${k}-hint`} className="muted">
          {hint}
        </span>
      )}
    </>
  );
  return (
    <ReasonForm
      title="New principal"
      submitLabel="Create"
      onCancel={onCancel}
      onSubmit={(reason) => createPrincipal({ ...p, handle: p.handle.trim().toLowerCase(), reason }).then((c) => onDone(`Created ${c.handle}. Grant roles before they start work.`))}
    >
      <label htmlFor="new-kind">Kind</label>
      <select id="new-kind" value={p.kind} onChange={(e) => setP({ ...p, kind: e.target.value as PrincipalKind })}>
        <option value="human">Human</option>
        <option value="service">Service</option>
        <option value="agent">Agent</option>
      </select>
      {field('handle', 'Handle', 'Lowercase letters, digits and . _ @ + -. Never reused.')}
      {field('display_name', 'Display name')}
      {field('email', 'Email')}
      {field('issuer', 'Identity provider issuer', 'The iss claim of their tokens; "dev" on a development server.')}
      {field('subject', 'Subject', 'The sub claim of their tokens; on a development server, the handle.')}
    </ReasonForm>
  );
}
