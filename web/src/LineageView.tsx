import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import cytoscape from 'cytoscape';
import dagre from 'cytoscape-dagre';
import { getLineage, type Lineage, type LineageDirection, type NodeKind } from './api';
import { tabKey, type Tab } from './tabs';

cytoscape.use(dagre);

type Props = {
  id: string;
  open: (t: Tab) => void;
  rename: (key: string, title: string) => void;
};

type Fetched = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; lineage: Lineage };

const directions: { value: LineageDirection; label: string }[] = [
  { value: 'upstream', label: 'Upstream' },
  { value: 'downstream', label: 'Downstream' },
  { value: 'both', label: 'Both' },
];

const kinds: NodeKind[] = ['sample', 'reagent', 'protocol', 'run', 'instrument', 'dataset', 'analysis'];

export default function LineageView({ id, open, rename }: Props) {
  const [direction, setDirection] = useState<LineageDirection>('both');
  const [fetched, setFetched] = useState<Fetched>({ status: 'loading' });
  const [selected, setSelected] = useState<string | null>(null);
  const canvas = useRef<HTMLDivElement>(null);
  const cy = useRef<cytoscape.Core | null>(null);
  const listbox = useRef<HTMLDivElement>(null);
  const lineage = fetched.status === 'ready' ? fetched.lineage : null;

  useEffect(() => {
    let live = true;
    setFetched({ status: 'loading' });
    getLineage(id, direction)
      .then((l) => {
        if (!live) return;
        setFetched({ status: 'ready', lineage: l });
        setSelected(l.root);
        const root = l.nodes.find((n) => n.id === l.root);
        if (root) rename(tabKey({ kind: 'lineage', id }), root.label);
      })
      .catch((err: unknown) => {
        if (live) setFetched({ status: 'error', message: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      live = false;
    };
  }, [id, direction, rename]);

  // Build the graph. The cleanup destroys it, so StrictMode's second run leaves one canvas.
  useEffect(() => {
    if (!lineage || !canvas.current) return;
    const css = getComputedStyle(document.documentElement);
    const token = (name: string) => css.getPropertyValue(name).trim();
    const instance = cytoscape({
      container: canvas.current,
      elements: [
        ...lineage.nodes.map((n) => ({ data: { id: n.id, label: n.label, kind: n.kind } })),
        ...(lineage.edges ?? []).map((e, i) => ({ data: { id: `e${i}`, source: e.from, target: e.to, relation: e.relation } })),
      ],
      layout: { name: 'dagre', rankDir: 'LR' } as cytoscape.LayoutOptions,
      minZoom: 0.1,
      maxZoom: 3,
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            color: token('--fg'),
            'font-size': 11,
            'text-valign': 'bottom',
            'text-margin-y': 4,
            'background-color': token('--muted'),
            'border-width': 0,
            'border-color': token('--fg'),
          },
        },
        ...kinds.map((k) => ({ selector: `node[kind = "${k}"]`, style: { 'background-color': token(`--kind-${k}`) } })),
        { selector: 'node.root', style: { 'border-width': 3 } },
        { selector: 'node.sel', style: { 'border-width': 3, 'border-color': token('--accent'), 'border-style': 'double' } },
        {
          selector: 'edge',
          style: {
            width: 1.5,
            'line-color': token('--border'),
            'target-arrow-color': token('--border'),
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
          },
        },
      ],
    });
    instance.getElementById(lineage.root).addClass('root');
    instance.on('tap', 'node', (ev) => setSelected(ev.target.id()));
    cy.current = instance;
    return () => {
      instance.destroy();
      cy.current = null;
    };
  }, [lineage]);

  // Highlight the selected node in the graph.
  useEffect(() => {
    cy.current?.nodes().removeClass('sel');
    if (selected) cy.current?.getElementById(selected).addClass('sel');
  }, [selected, lineage]);

  useEffect(() => {
    if (selected) listbox.current?.querySelector(`[id="opt-${selected}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [selected]);

  function onListKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (!lineage) return;
    const i = lineage.nodes.findIndex((n) => n.id === selected);
    if (e.key === 'ArrowDown') setSelected(lineage.nodes[Math.min(i + 1, lineage.nodes.length - 1)].id);
    else if (e.key === 'ArrowUp') setSelected(lineage.nodes[Math.max(i - 1, 0)].id);
    else if (e.key === 'Enter' && i >= 0) recentre(lineage.nodes[i].id, lineage.nodes[i].label);
    else return;
    e.preventDefault();
  }

  function recentre(nodeId: string, label: string) {
    open({ kind: 'lineage', id: nodeId, title: label });
  }

  const node = lineage?.nodes.find((n) => n.id === selected);

  return (
    <div className="lineage">
      <div className="lineage-main">
        <div role="radiogroup" aria-label="Direction" className="lineage-direction">
          {directions.map((d) => (
            <label key={d.value}>
              <input type="radio" name={`direction-${id}`} checked={direction === d.value} onChange={() => setDirection(d.value)} /> {d.label}
            </label>
          ))}
        </div>
        {fetched.status === 'error' && <p role="alert">{fetched.message}</p>}
        {fetched.status === 'loading' && <p className="muted">Loading lineage…</p>}
        {/* Cytoscape injects a <style> element (blocked by our CSP) unless an element with this id exists.
            .lineage-canvas already sets position: relative, which is all that style would do. */}
        <template id="__________cytoscape_stylesheet" />
        <div className="lineage-canvas" ref={canvas} data-nodes={lineage?.nodes.length ?? 0} />
      </div>
      {lineage && (
        <aside className="lineage-side" aria-label="Lineage details">
          <h2>Nodes</h2>
          <div
            role="listbox"
            aria-label="Nodes"
            tabIndex={0}
            ref={listbox}
            className="lineage-list"
            aria-activedescendant={selected ? `opt-${selected}` : undefined}
            onKeyDown={onListKeyDown}
          >
            {lineage.nodes.map((n) => (
              <div key={n.id} id={`opt-${n.id}`} role="option" aria-selected={n.id === selected} onClick={() => setSelected(n.id)}>
                {n.label} <span className="muted">({n.kind})</span>
              </div>
            ))}
          </div>
          {node && (
            <section aria-label="Selected node">
              <h2>{node.label}</h2>
              <p>
                Kind: <strong>{node.kind}</strong>
              </p>
              <p>Created: {node.created_at}</p>
              <pre className="mono">{JSON.stringify(node.attrs ?? {}, null, 2)}</pre>
              <button type="button" onClick={() => recentre(node.id, node.label)}>
                Re-centre here
              </button>
            </section>
          )}
        </aside>
      )}
    </div>
  );
}
