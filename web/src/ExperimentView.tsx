import MarkdownIt from 'markdown-it';
import { useEffect, useState } from 'react';
import { getExperiment, listProtocolVersions, type Experiment, type ProtocolVersion } from './api';
import { tabKey, type Tab } from './tabs';

// HTML is disabled, so rendered output cannot contain author-supplied markup.
const md = new MarkdownIt({ html: false, linkify: false });

type Props = { id: string; open: (t: Tab) => void; rename: (key: string, title: string) => void };

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; exp: Experiment; versions: ProtocolVersion[] };

function show(v: unknown): string {
  return typeof v === 'string' ? v : JSON.stringify(v);
}

export default function ExperimentView({ id, open, rename }: Props) {
  const [state, setState] = useState<State>({ status: 'loading' });

  useEffect(() => {
    let live = true;
    setState({ status: 'loading' });
    Promise.all([getExperiment(id), listProtocolVersions(id)])
      .then(([exp, versions]) => {
        if (!live) return;
        setState({ status: 'ready', exp, versions });
        rename(tabKey({ kind: 'experiment', id }), exp.slug);
      })
      .catch((err: unknown) => {
        if (live) setState({ status: 'error', message: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      live = false;
    };
  }, [id, rename]);

  if (state.status === 'loading') return <p className="muted">Loading experiment…</p>;
  if (state.status === 'error') return <p role="alert" className="error">{state.message}</p>;

  const { exp, versions } = state;
  const current =
    versions.find((v) => v.id === exp.protocol_version_id) ??
    versions.reduce<ProtocolVersion | undefined>((a, v) => (!a || v.version > a.version ? v : a), undefined);
  const meta =
    exp.protocol_meta && typeof exp.protocol_meta === 'object' && !Array.isArray(exp.protocol_meta)
      ? Object.entries(exp.protocol_meta as Record<string, unknown>)
      : [];

  return (
    <article className="view">
      <h1>{exp.title}</h1>
      <p className="muted">
        <span className="mono">{exp.slug}</span>
      </p>
      <p>
        Version {exp.protocol_version} <span className="mono hash">{exp.protocol_sha256}</span>
      </p>
      {current && (
        <p>
          <button
            type="button"
            onClick={() => open({ kind: 'lineage', id: current.node_id, title: `${exp.slug} v${current.version}` })}
          >
            Open lineage
          </button>
        </p>
      )}

      <h2>Protocol</h2>
      <div className="protocol" dangerouslySetInnerHTML={{ __html: md.render(exp.protocol_md) }} />

      <h2>Metadata</h2>
      {meta.length === 0 ? (
        <p className="muted">No metadata.</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Protocol metadata</caption>
          <thead>
            <tr>
              <th scope="col">Key</th>
              <th scope="col">Value</th>
            </tr>
          </thead>
          <tbody>
            {meta.map(([k, v]) => (
              <tr key={k}>
                <th scope="row">{k}</th>
                <td className="mono">{show(v)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>Versions</h2>
      <table className="data">
        <caption className="sr-only">Protocol versions</caption>
        <thead>
          <tr>
            <th scope="col">Version</th>
            <th scope="col">Hash</th>
            <th scope="col">Created by</th>
            <th scope="col">Created at</th>
          </tr>
        </thead>
        <tbody>
          {versions.map((v) => (
            <tr key={v.id}>
              <td>{v.version}</td>
              <td className="mono">{v.sha256.slice(0, 12)}</td>
              <td>{v.created_by}</td>
              <td>{v.created_at}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </article>
  );
}
