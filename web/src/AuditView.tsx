import { useEffect, useState } from 'react';
import { listAuditEvents, verifyAudit, type AuditEvent, type AuditVerify } from './api';

// verifyOnOpen is a counter: each new non-zero value runs the chain verification (command palette, 3.9).
type Props = { id: string; verifyOnOpen?: number };

type Page = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; events: AuditEvent[] };
type Verify = { status: 'idle' } | { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; result: AuditVerify };

const PAGE = 100;

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export default function AuditView({ verifyOnOpen }: Props) {
  const [after, setAfter] = useState(0);
  const [filter, setFilter] = useState<{ type: string; id: string } | null>(null);
  const [type, setType] = useState('');
  const [entityId, setEntityId] = useState('');
  const [page, setPage] = useState<Page>({ status: 'loading' });
  const [verify, setVerify] = useState<Verify>({ status: 'idle' });

  useEffect(() => {
    let live = true;
    setPage({ status: 'loading' });
    listAuditEvents(after, filter?.type, filter?.id)
      .then((events) => live && setPage({ status: 'ready', events }))
      .catch((err: unknown) => live && setPage({ status: 'error', message: message(err) }));
    return () => {
      live = false;
    };
  }, [after, filter]);

  function runVerify() {
    setVerify({ status: 'loading' });
    verifyAudit()
      .then((result) => setVerify({ status: 'ready', result }))
      .catch((err: unknown) => setVerify({ status: 'error', message: message(err) }));
  }

  useEffect(() => {
    if (verifyOnOpen) runVerify();
  }, [verifyOnOpen]);

  function applyFilter() {
    setAfter(0);
    setFilter({ type: type.trim(), id: entityId.trim() });
  }

  function clearFilter() {
    setType('');
    setEntityId('');
    setAfter(0);
    setFilter(null);
  }

  const events = page.status === 'ready' ? page.events : [];
  const canFilter = type.trim() !== '' && entityId.trim() !== '';
  const brokenAt = verify.status === 'ready' ? (verify.result.engine.break?.seq ?? verify.result.database.broken_at) : undefined;

  return (
    <article className="view">
      <h1>Audit trail</h1>

      <h2>Verify</h2>
      <p>
        <button type="button" onClick={runVerify}>
          Verify chain
        </button>
      </p>
      <div aria-live="polite">
        {verify.status === 'loading' && <p className="muted">Verifying…</p>}
        {verify.status === 'ready' && (
          <>
            <p>
              <strong>{verify.result.valid ? 'Chain valid' : `Chain broken at seq ${brokenAt}`}</strong>
            </p>
            <p>
              Engine head: seq {verify.result.engine.head.seq} <span className="mono hash">{verify.result.engine.head.hash}</span>
            </p>
            <p>
              Database head: seq {verify.result.database.head_seq} <span className="mono hash">{verify.result.database.head_hash}</span>
            </p>
            <p>{verify.result.agree ? 'Engine and database agree' : 'Engine and database disagree'}</p>
          </>
        )}
      </div>
      {verify.status === 'error' && <p role="alert" className="error">{verify.message}</p>}

      <h2>Events</h2>
      <form
        className="diff-controls"
        onSubmit={(e) => {
          e.preventDefault();
          if (canFilter) applyFilter();
        }}
      >
        <label>
          Entity type <input type="text" value={type} onChange={(e) => setType(e.target.value)} />
        </label>
        <label>
          Entity id <input type="text" value={entityId} onChange={(e) => setEntityId(e.target.value)} />
        </label>
        <button type="submit" disabled={!canFilter}>
          Filter
        </button>
        <button type="button" onClick={clearFilter}>
          Clear
        </button>
      </form>

      {page.status === 'loading' && <p className="muted">Loading events…</p>}
      {page.status === 'error' && <p role="alert" className="error">{page.message}</p>}
      {page.status === 'ready' && events.length === 0 && <p className="muted">No events</p>}
      {events.length > 0 && (
        <table className="data">
          <caption className="sr-only">Audit events</caption>
          <thead>
            <tr>
              <th scope="col">Seq</th>
              <th scope="col">Time</th>
              <th scope="col">Actor</th>
              <th scope="col">Action</th>
              <th scope="col">Entity type</th>
              <th scope="col">Entity id</th>
              <th scope="col">Reason</th>
            </tr>
          </thead>
          <tbody>
            {events.map((ev) => (
              <tr key={ev.seq}>
                <th scope="row">{ev.seq}</th>
                <td>{ev.occurred_at}</td>
                <td>{ev.actor}</td>
                <td>{ev.action}</td>
                <td>{ev.entity_type}</td>
                <td className="mono">{ev.entity_id}</td>
                <td>{ev.reason ?? ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <p className="diff-controls">
        <button type="button" disabled={after === 0} onClick={() => setAfter(0)}>
          First page
        </button>
        <button type="button" disabled={events.length < PAGE} onClick={() => setAfter(events[events.length - 1].seq)}>
          Next page
        </button>
      </p>
    </article>
  );
}
