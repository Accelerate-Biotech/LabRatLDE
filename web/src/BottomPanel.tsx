import { useEffect, useState, type KeyboardEvent } from 'react';
import { listAuditEvents, type AuditEvent } from './api';
import { tabKey, type Tab } from './tabs';

const PANELS = ['Problems', 'History'] as const;

// The audit entity_type for each tab kind; the audit tab has no single entity.
const ENTITY_TYPE = { experiment: 'experiment', run: 'run', instrument: 'instrument', lineage: 'lineage_node' } as const;

function History({ tab }: { tab: Tab | null }) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const key = tab ? tabKey(tab) : null;

  useEffect(() => {
    setEvents([]);
    setError(null);
    if (!tab || tab.kind === 'audit') {
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

  if (!tab || tab.kind === 'audit') return <p className="muted">Select an entity to see its history</p>;
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

export default function BottomPanel({ active }: { active: Tab | null }) {
  const [panel, setPanel] = useState<(typeof PANELS)[number]>('Problems');

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
      <div role="tabpanel" id="panel-body" aria-labelledby={`panel-tab-${panel}`} className="panel-body">
        {panel === 'Problems' ? <p className="muted">No problems</p> : <History tab={active} />}
      </div>
    </section>
  );
}
