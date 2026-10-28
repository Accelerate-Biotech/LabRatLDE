import { useEffect, useState, type KeyboardEvent, type ReactNode } from 'react';
import {
  listExperiments,
  listInstruments,
  listRuns,
  type ExperimentSummary,
  type Instrument,
  type Page,
  type RunSummary,
} from './api';
import type { Tab } from './tabs';

type List<T> = { items: T[]; next: string | null; loading: boolean; error: string | null };

const empty = { items: [], next: null, loading: false, error: null };

// Everything the explorer has loaded so far, for the command palette and views.
export type Loaded = {
  experiments: ExperimentSummary[];
  runs: Record<string, RunSummary[]>;
  instruments: Instrument[];
};

// load fetches one page into a list: no cursor replaces, a cursor appends.
function load<T>(
  set: (update: (l: List<T>) => List<T>) => void,
  fetchPage: (cursor?: string) => Promise<Page<T>>,
  cursor?: string,
) {
  set((l) => ({ ...l, loading: true, error: null }));
  fetchPage(cursor)
    .then((p) => set((l) => ({ items: cursor ? [...l.items, ...p.items] : p.items, next: p.next_cursor, loading: false, error: null })))
    .catch((err: unknown) => set((l) => ({ ...l, loading: false, error: err instanceof Error ? err.message : String(err) })));
}

type ItemProps = {
  itemKey: string;
  level: number;
  focusKey: string;
  expanded?: boolean; // undefined: a leaf
  onToggle?: () => void;
  onActivate: () => void;
  label: string;
  children?: ReactNode;
};

function Item({ itemKey, level, focusKey, expanded, onToggle, onActivate, label, children }: ItemProps) {
  function onKeyDown(e: KeyboardEvent<HTMLLIElement>) {
    if (e.target !== e.currentTarget) return;
    const el = e.currentTarget;
    const items = Array.from(el.closest('[role="tree"]')!.querySelectorAll<HTMLElement>('[role="treeitem"]'));
    const i = items.indexOf(el);
    switch (e.key) {
      case 'ArrowDown':
        items[i + 1]?.focus();
        break;
      case 'ArrowUp':
        items[i - 1]?.focus();
        break;
      case 'ArrowRight':
        if (expanded === false) onToggle?.();
        else if (expanded) items[i + 1]?.focus();
        break;
      case 'ArrowLeft':
        if (expanded) onToggle?.();
        else el.parentElement?.closest<HTMLElement>('[role="treeitem"]')?.focus();
        break;
      case 'Enter':
        onActivate();
        break;
      default:
        return;
    }
    e.preventDefault();
    e.stopPropagation();
  }

  return (
    <li
      role="treeitem"
      aria-level={level}
      aria-expanded={expanded}
      tabIndex={itemKey === focusKey ? 0 : -1}
      data-key={itemKey}
      onKeyDown={onKeyDown}
    >
      <span className="tree-label" onClick={onActivate}>
        <span
          aria-hidden="true"
          onClick={(e) => {
            if (onToggle) {
              e.stopPropagation();
              onToggle();
            }
          }}
        >
          {expanded === undefined ? '•' : expanded ? '▾' : '▸'}
        </span>{' '}
        {label}
      </span>
      {children}
    </li>
  );
}

function Status<T>({ list }: { list: List<T> }) {
  return (
    <>
      {list.loading && (
        <li role="none" className="muted">
          Loading…
        </li>
      )}
      {list.error && (
        <li role="none">
          <span role="alert">{list.error}</span>
        </li>
      )}
    </>
  );
}

export default function Explorer({ open, onLoaded }: { open: (tab: Tab) => void; onLoaded: (l: Loaded) => void }) {
  const [experiments, setExperiments] = useState<List<ExperimentSummary>>(empty);
  const [instruments, setInstruments] = useState<List<Instrument>>(empty);
  const [runs, setRuns] = useState<Record<string, List<RunSummary>>>({});
  const [expanded, setExpanded] = useState<Record<string, boolean>>({ instruments: true });
  const [focusKey, setFocusKey] = useState<string | null>(null);

  useEffect(() => {
    load(setExperiments, listExperiments);
    load(setInstruments, listInstruments);
  }, []);

  useEffect(() => {
    onLoaded({
      experiments: experiments.items,
      runs: Object.fromEntries(Object.entries(runs).map(([id, l]) => [id, l.items])),
      instruments: instruments.items,
    });
  }, [experiments.items, runs, instruments.items, onLoaded]);

  function loadRuns(id: string, cursor?: string) {
    load<RunSummary>((u) => setRuns((r) => ({ ...r, [id]: u(r[id] ?? empty) })), (c) => listRuns(id, c), cursor);
  }

  function toggle(key: string) {
    setExpanded((x) => ({ ...x, [key]: !x[key] }));
  }

  function toggleExperiment(id: string) {
    if (!expanded[id] && !runs[id]) loadRuns(id);
    toggle(id);
  }

  const fk = focusKey ?? (experiments.items[0] ? `e:${experiments.items[0].id}` : 'instruments');

  return (
    <ul role="tree" aria-label="Explorer" className="tree" onFocus={(e) => setFocusKey((e.target as HTMLElement).dataset.key ?? null)}>
      {experiments.items.map((x) => {
        const r = runs[x.id] ?? empty;
        return (
          <Item
            key={x.id}
            itemKey={`e:${x.id}`}
            level={1}
            focusKey={fk}
            expanded={!!expanded[x.id]}
            onToggle={() => toggleExperiment(x.id)}
            onActivate={() => open({ kind: 'experiment', id: x.id, title: x.slug })}
            label={x.slug}
          >
            {expanded[x.id] && (
              <ul role="group">
                {r.items.map((run) => (
                  <Item
                    key={run.id}
                    itemKey={`r:${run.id}`}
                    level={2}
                    focusKey={fk}
                    onActivate={() => open({ kind: 'run', id: run.id, title: run.label })}
                    label={`${run.label} (${run.state})`}
                  />
                ))}
                {r.next && (
                  <Item itemKey={`more:r:${x.id}`} level={2} focusKey={fk} onActivate={() => loadRuns(x.id, r.next!)} label="Load more" />
                )}
                <Status list={r} />
                {!r.loading && !r.error && r.items.length === 0 && (
                  <li role="none" className="muted">
                    No runs
                  </li>
                )}
              </ul>
            )}
          </Item>
        );
      })}
      {experiments.next && (
        <Item itemKey="more:e" level={1} focusKey={fk} onActivate={() => load(setExperiments, listExperiments, experiments.next!)} label="Load more" />
      )}
      <Status list={experiments} />

      <Item
        itemKey="instruments"
        level={1}
        focusKey={fk}
        expanded={!!expanded.instruments}
        onToggle={() => toggle('instruments')}
        onActivate={() => toggle('instruments')}
        label="Instruments"
      >
        {expanded.instruments && (
          <ul role="group">
            {instruments.items.map((i) => (
              <Item
                key={i.id}
                itemKey={`i:${i.id}`}
                level={2}
                focusKey={fk}
                onActivate={() => open({ kind: 'instrument', id: i.id, title: i.name })}
                label={i.name}
              />
            ))}
            {instruments.next && (
              <Item
                itemKey="more:i"
                level={2}
                focusKey={fk}
                onActivate={() => load(setInstruments, listInstruments, instruments.next!)}
                label="Load more"
              />
            )}
            <Status list={instruments} />
          </ul>
        )}
      </Item>

      <Item
        itemKey="audit"
        level={1}
        focusKey={fk}
        onActivate={() => open({ kind: 'audit', id: 'audit', title: 'Audit trail' })}
        label="Audit trail"
      />
    </ul>
  );
}
