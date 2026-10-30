import { useEffect, useState, type KeyboardEvent } from 'react';
import {
  blame,
  diffRevisions,
  listAuditEvents,
  listRevisions,
  type AuditEvent,
  type BlameEntry,
  type Change,
  type RecordType,
  type Revision,
} from './api';
import { tabKey, type Kind, type Tab } from './tabs';

const PANELS = ['Problems', 'History', 'Revisions'] as const;

// The audit entity_type for each tab kind; the audit tab has no single entity.
const ENTITY_TYPE: Partial<Record<Kind, string>> = { experiment: 'experiment', run: 'run', instrument: 'instrument', lineage: 'lineage_node' };

// The record type for each tab kind that has revisions.
const RECORD_TYPE: Partial<Record<Kind, RecordType>> = { experiment: 'experiment', run: 'run', lineage: 'lineage_node' };

function History({ tab }: { tab: Tab | null }) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const key = tab ? tabKey(tab) : null;

  useEffect(() => {
    setEvents([]);
    setError(null);
    if (!tab || !ENTITY_TYPE[tab.kind]) {
      setLoading(false);
      return;
    }
    let live = true;
    setLoading(true);
    listAuditEvents(0, ENTITY_TYPE[tab.kind], tab.id)
      .then((ev) => {
        if (live) setEvents(ev);
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
    // Refetch when the active tab changes, not when its title does.
  }, [key]);

  if (!tab || !ENTITY_TYPE[tab.kind]) return <p className="muted">Select an entity to see its history</p>;
  if (error) return <p role="alert">{error}</p>;
  if (loading) return <p className="muted">Loading…</p>;
  if (events.length === 0) return <p className="muted">No events</p>;
  return (
    <table className="history">
      <thead>
        <tr>
          <th scope="col">Time</th>
          <th scope="col">Actor</th>
          <th scope="col">Action</th>
          <th scope="col">Reason</th>
        </tr>
      </thead>
      <tbody>
        {events.map((e) => (
          <tr key={e.seq}>
            <td>{new Date(e.occurred_at).toLocaleString()}</td>
            <td>{e.actor}</td>
            <td>{e.action}</td>
            <td>{e.reason ?? ''}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// Revisions lists a record's revisions, lets the user pick two, and shows the diff between them.
// The parent keys it by tab, so a tab change starts with fresh state.
function Revisions({ tab }: { tab: Tab | null }) {
  const type = tab ? RECORD_TYPE[tab.kind] : undefined;
  const [revs, setRevs] = useState<Revision[]>([]);
  const [blamed, setBlamed] = useState<BlameEntry[]>([]);
  const [from, setFrom] = useState<number | null>(null);
  const [to, setTo] = useState<number | null>(null);
  const [changes, setChanges] = useState<Change[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [reload, setReload] = useState(0);

  // A successful edit adds a revision, so reload.
  useEffect(() => {
    const onRevised = () => setReload((n) => n + 1);
    window.addEventListener('labrat:revised', onRevised);
    return () => window.removeEventListener('labrat:revised', onRevised);
  }, []);

  useEffect(() => {
    if (!tab || !type) {
      setLoading(false);
      return;
    }
    let live = true;
    setLoading(true);
    setError(null);
    Promise.all([listRevisions(type, tab.id), blame(type, tab.id)])
      .then(([r, b]) => {
        if (!live) return;
        // The server lists revisions oldest first. Default to the previous and the latest.
        const items = r.items;
        const latest = items.length > 0 ? items[items.length - 1].rev : null;
        const previous = items.length > 1 ? items[items.length - 2].rev : latest;
        setRevs(items);
        setBlamed(b.fields);
        setFrom(previous);
        setTo(latest);
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
    // Refetch when the active tab changes (the parent keys by tab) or a revision is saved.
  }, [reload]);

  useEffect(() => {
    if (!tab || !type || from === null || to === null) return;
    let live = true;
    setChanges(null);
    diffRevisions(type, tab.id, from, to)
      .then((d) => {
        if (live) setChanges(d.changes);
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      live = false;
    };
    // Recompute the diff when the selection changes or a revision is saved.
  }, [from, to, reload]);

  if (!tab || !type) return <p className="muted">Select a record to see its revisions</p>;
  if (error) return <p role="alert">{error}</p>;
  if (loading) return <p className="muted">Loading…</p>;
  if (revs.length === 0) return <p className="muted">No revisions</p>;

  return (
    <>
      <table className="history" aria-label="Revisions">
        <thead>
          <tr>
            <th scope="col">From</th>
            <th scope="col">To</th>
            <th scope="col">Rev</th>
            <th scope="col">Time</th>
            <th scope="col">Author</th>
            <th scope="col">Reason</th>
          </tr>
        </thead>
        <tbody>
          {revs.map((r) => (
            <tr key={r.rev}>
              <td>
                <input
                  type="radio"
                  name="revision-from"
                  aria-label={`From revision ${r.rev}`}
                  checked={from === r.rev}
                  onChange={() => setFrom(r.rev)}
                />
              </td>
              <td>
                <input
                  type="radio"
                  name="revision-to"
                  aria-label={`To revision ${r.rev}`}
                  checked={to === r.rev}
                  onChange={() => setTo(r.rev)}
                />
              </td>
              <td>{r.rev}</td>
              <td>{new Date(r.created_at).toLocaleString()}</td>
              <td>{r.author}</td>
              <td>{r.reason}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {changes === null ? (
        <p className="muted">Loading…</p>
      ) : changes.length === 0 ? (
        <p className="muted">No differences</p>
      ) : (
        <table className="history changes" aria-label="Changes">
          <thead>
            <tr>
              <th scope="col">Path</th>
              <th scope="col">Kind</th>
              <th scope="col">From</th>
              <th scope="col">To</th>
            </tr>
          </thead>
          <tbody>
            {changes.map((c) => {
              const b = blamed.find((e) => e.path === c.path);
              return (
                <tr key={c.path}>
                  <td title={b ? `rev ${b.rev} · ${b.author} · ${new Date(b.at).toLocaleString()}` : undefined}>{c.path}</td>
                  <td>{c.kind}</td>
                  <td>{c.from === undefined ? '' : JSON.stringify(c.from)}</td>
                  <td>{c.to === undefined ? '' : JSON.stringify(c.to)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </>
  );
}

export default function BottomPanel({ active }: { active: Tab | null }) {
  const [panel, setPanel] = useState<(typeof PANELS)[number]>('Problems');

  // The "changed" marker on an edit field asks for the Revisions panel.
  useEffect(() => {
    const show = () => setPanel('Revisions');
    window.addEventListener('labrat:show-revisions', show);
    return () => window.removeEventListener('labrat:show-revisions', show);
  }, []);

  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    const i = PANELS.indexOf(panel);
    let next: number;
    if (e.key === 'ArrowRight') next = (i + 1) % PANELS.length;
    else if (e.key === 'ArrowLeft') next = (i + PANELS.length - 1) % PANELS.length;
    else return;
    e.preventDefault();
    setPanel(PANELS[next]);
    document.getElementById(`panel-tab-${PANELS[next]}`)?.focus();
  }

  return (
    <section className="bottom" aria-label="Panel">
      <div role="tablist" aria-label="Panel">
        {PANELS.map((p) => (
          <button
            key={p}
            type="button"
            role="tab"
            id={`panel-tab-${p}`}
            aria-selected={p === panel}
            aria-controls="panel-body"
            tabIndex={p === panel ? 0 : -1}
            onClick={() => setPanel(p)}
            onKeyDown={onKeyDown}
          >
            {p}
          </button>
        ))}
      </div>
      <div role="tabpanel" id="panel-body" aria-labelledby={`panel-tab-${panel}`} className="panel-body" tabIndex={0}>
        {panel === 'Problems' && <p className="muted">No problems</p>}
        {panel === 'History' && <History tab={active} />}
        {panel === 'Revisions' && <Revisions key={active ? tabKey(active) : ''} tab={active} />}
      </div>
    </section>
  );
}
