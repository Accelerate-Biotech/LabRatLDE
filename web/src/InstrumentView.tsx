import { useEffect, useState } from 'react';
import { listInstruments, type Instrument } from './api';
import { tabKey, type Tab } from './tabs';

// instruments is the explorer's already-loaded list; the view only pages the API when the id is not in it.
type Props = {
  id: string;
  instruments: Instrument[];
  open: (t: Tab) => void;
  rename: (key: string, title: string) => void;
};

type Fetched = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; instrument: Instrument };

function show(v: unknown): string {
  return typeof v === 'string' ? v : JSON.stringify(v);
}

export default function InstrumentView({ id, instruments, open, rename }: Props) {
  const fromList = instruments.find((i) => i.id === id);
  const inList = fromList !== undefined;
  const [fetched, setFetched] = useState<Fetched>({ status: 'loading' });

  // Depends on inList, not on the list itself, so growing the list does not restart the search.
  useEffect(() => {
    if (inList) return;
    let live = true;
    setFetched({ status: 'loading' });
    (async () => {
      let cursor: string | undefined;
      do {
        const page = await listInstruments(cursor);
        const found = page.items.find((i) => i.id === id);
        if (found) return setFetched({ status: 'ready', instrument: found });
        cursor = page.next_cursor ?? undefined;
      } while (cursor && live);
      setFetched({ status: 'error', message: 'Instrument not found' });
    })().catch((err: unknown) => {
      if (live) setFetched({ status: 'error', message: err instanceof Error ? err.message : String(err) });
    });
    return () => {
      live = false;
    };
  }, [id, inList]);

  const instrument = fromList ?? (fetched.status === 'ready' ? fetched.instrument : undefined);
  const name = instrument?.name;

  useEffect(() => {
    if (name !== undefined) rename(tabKey({ kind: 'instrument', id }), name);
  }, [id, name, rename]);

  if (!instrument) {
    if (fetched.status === 'error') return <p role="alert" className="error">{fetched.message}</p>;
    return <p className="muted">Loading instrument…</p>;
  }

  const attrs = Object.entries(instrument.attrs ?? {});

  return (
    <article className="view">
      <h1>{instrument.name}</h1>
      <p>
        Kind: <strong>{instrument.kind}</strong>
      </p>
      <p>
        <button type="button" onClick={() => open({ kind: 'lineage', id: instrument.node_id, title: instrument.name })}>
          Open lineage
        </button>
      </p>

      <h2>Attributes</h2>
      {attrs.length === 0 ? (
        <p className="muted">No attributes</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Instrument attributes</caption>
          <thead>
            <tr>
              <th scope="col">Key</th>
              <th scope="col">Value</th>
            </tr>
          </thead>
          <tbody>
            {attrs.map(([k, v]) => (
              <tr key={k}>
                <th scope="row">{k}</th>
                <td className="mono">{show(v)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </article>
  );
}
