import MarkdownIt from 'markdown-it';
import { useEffect, useState } from 'react';
import { getExperiment, getProtocolVersion, listProtocolVersions, type Experiment, type ProtocolVersion } from './api';
import { lineDiff, type DiffLine } from './diff';
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

// Drops a leading YAML frontmatter block; the metadata table already shows it.
function stripFrontmatter(text: string): string {
  const t = text.replace(/^﻿/, '').replace(/\r\n/g, '\n');
  const m = /^---\n[\s\S]*?\n---[ \t]*(\n|$)/.exec(t);
  return m ? t.slice(m[0].length) : t;
}

type Diff = { status: 'idle' } | { status: 'error'; message: string } | { status: 'done'; lines: DiffLine[] };

export default function ExperimentView({ id, open, rename }: Props) {
  const [state, setState] = useState<State>({ status: 'loading' });
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [diff, setDiff] = useState<Diff>({ status: 'idle' });

  useEffect(() => {
    let live = true;
    setState({ status: 'loading' });
    Promise.all([getExperiment(id), listProtocolVersions(id)])
      .then(([exp, versions]) => {
        if (!live) return;
        setState({ status: 'ready', exp, versions });
        const sorted = [...versions].sort((p, q) => p.version - q.version);
        const cur = sorted.find((v) => v.id === exp.protocol_version_id) ?? sorted[sorted.length - 1];
        const prev = [...sorted].reverse().find((v) => cur && v.version < cur.version) ?? cur;
        setFrom(prev?.id ?? '');
        setTo(cur?.id ?? '');
        setDiff({ status: 'idle' });
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

  const compare = () => {
    Promise.all([getProtocolVersion(from), getProtocolVersion(to)])
      .then(([a, b]) => setDiff({ status: 'done', lines: lineDiff(a.protocol_md ?? '', b.protocol_md ?? '') }))
      .catch((err: unknown) =>
        setDiff({ status: 'error', message: err instanceof Error ? err.message : String(err) }),
      );
  };

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
      <div className="protocol" dangerouslySetInnerHTML={{ __html: md.render(stripFrontmatter(exp.protocol_md)) }} />

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

      <h2>Compare versions</h2>
      {versions.length < 2 ? (
        <p className="muted">Needs two versions</p>
      ) : (
        <div className="diff-controls">
          <label>
            From version
            <select value={from} onChange={(e) => setFrom(e.target.value)}>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>
                  v{v.version}
                </option>
              ))}
            </select>
          </label>
          <label>
            To version
            <select value={to} onChange={(e) => setTo(e.target.value)}>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>
                  v{v.version}
                </option>
              ))}
            </select>
          </label>
          <button type="button" onClick={compare}>
            Compare
          </button>
        </div>
      )}
      {diff.status === 'error' && (
        <p role="alert" className="error">
          {diff.message}
        </p>
      )}
      {diff.status === 'done' &&
        (diff.lines.every((l) => l.op === ' ') ? (
          <p>No differences</p>
        ) : (
          <pre className="diff" aria-label="Protocol diff">
            {diff.lines.map((l, i) => (
              <span key={i} className={l.op === '+' ? 'diff-add' : l.op === '-' ? 'diff-del' : undefined}>
                {l.op + ' ' + l.line}
              </span>
            ))}
          </pre>
        ))}
    </article>
  );
}
