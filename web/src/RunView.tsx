import { useEffect, useState } from 'react';
import { getRun, listAuditEvents, type AuditEvent, type RunDetail } from './api';
import type { Tab } from './tabs';

// The explorer supplies the tab title (the run label); RunDetail has none, so rename is unused.
type Props = { id: string; open: (t: Tab) => void; rename: (key: string, title: string) => void };

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; run: RunDetail; events: AuditEvent[] };

function show(v: unknown): string {
  return typeof v === 'string' ? v : JSON.stringify(v);
}

export default function RunView({ id, open }: Props) {
  const [state, setState] = useState<State>({ status: 'loading' });

  useEffect(() => {
    let live = true;
    setState({ status: 'loading' });
    Promise.all([getRun(id), listAuditEvents(0, 'run', id)])
      .then(([run, events]) => {
        if (live) setState({ status: 'ready', run, events });
      })
      .catch((err: unknown) => {
        if (live) setState({ status: 'error', message: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      live = false;
    };
  }, [id]);

  if (state.status === 'loading') return <p className="muted">Loading run…</p>;
  if (state.status === 'error') return <p role="alert" className="error">{state.message}</p>;

  const { run, events } = state;
  const params =
    run.params && typeof run.params === 'object' && !Array.isArray(run.params)
      ? Object.entries(run.params as Record<string, unknown>)
      : null;

  return (
    <article className="view">
      <h1>Run</h1>
      <p>
        State: <strong>{run.state}</strong>
      </p>
      <p>Started: {run.started_at ?? 'not started'}</p>
      <p>Finished: {run.finished_at ?? 'not finished'}</p>
      <p>
        <button type="button" onClick={() => open({ kind: 'lineage', id: run.node_id, title: 'run' })}>
          Open lineage
        </button>
      </p>

      <h2>Protocol</h2>
      <p>
        v{run.protocol_version} <span className="mono hash">{run.protocol_sha256}</span>
      </p>

      <h2>Params</h2>
      {params === null ? (
        <p className="mono">{show(run.params)}</p>
      ) : params.length === 0 ? (
        <p className="muted">No params.</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Run params</caption>
          <thead>
            <tr>
              <th scope="col">Key</th>
              <th scope="col">Value</th>
            </tr>
          </thead>
          <tbody>
            {params.map(([k, v]) => (
              <tr key={k}>
                <th scope="row">{k}</th>
                <td className="mono">{show(v)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>Inputs</h2>
      {run.inputs.length === 0 ? (
        <p className="muted">No inputs</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Run inputs</caption>
          <thead>
            <tr>
              <th scope="col">Kind</th>
              <th scope="col">Label</th>
            </tr>
          </thead>
          <tbody>
            {run.inputs.map((i) => (
              <tr key={i.node_id}>
                <td>{i.kind}</td>
                <td>{i.label}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>Datasets</h2>
      {run.datasets.length === 0 ? (
        <p className="muted">No datasets</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Generated datasets</caption>
          <thead>
            <tr>
              <th scope="col">Label</th>
              <th scope="col">Readings</th>
              <th scope="col">SHA-256</th>
            </tr>
          </thead>
          <tbody>
            {run.datasets.map((d) => (
              <tr key={d.node_id}>
                <td>{d.label}</td>
                <td>{d.count ?? 'not recorded'}</td>
                <td className="mono hash">{d.sha256 ?? 'not recorded'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>State history</h2>
      {events.length === 0 ? (
        <p className="muted">No events</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Run state history</caption>
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
                <td>{e.occurred_at}</td>
                <td>{e.actor}</td>
                <td>{e.action}</td>
                <td>{e.reason ?? ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </article>
  );
}
