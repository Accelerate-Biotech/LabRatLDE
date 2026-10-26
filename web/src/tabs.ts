import { useCallback, useEffect, useState } from 'react';

export type Kind = 'experiment' | 'run' | 'instrument' | 'lineage' | 'audit';

export type Tab = { kind: Kind; id: string; title: string };

const KINDS: Kind[] = ['experiment', 'run', 'instrument', 'lineage', 'audit'];

export function tabKey(tab: Pick<Tab, 'kind' | 'id'>): string {
  return `${tab.kind}:${tab.id}`;
}

// fromHash reads "#/<kind>/<id>" (or "#/audit"). The hash carries no title, so
// the tab gets a placeholder that the view renames once it has loaded.
function fromHash(hash: string): Tab | null {
  const m = /^#\/([a-z]+)(?:\/(.+))?$/.exec(hash);
  if (!m) return null;
  const kind = KINDS.find((k) => k === m[1]);
  if (!kind) return null;
  if (kind === 'audit') return { kind, id: 'audit', title: 'Audit trail' };
  if (!m[2]) return null;
  let id: string;
  try {
    id = decodeURIComponent(m[2]);
  } catch {
    return null;
  }
  return { kind, id, title: `${kind} ${id.slice(0, 8)}` };
}

function toHash(tab: Tab): string {
  return `#/${tab.kind}/${encodeURIComponent(tab.id)}`;
}

export function useTabs() {
  const [tabs, setTabs] = useState<Tab[]>(() => {
    const t = fromHash(location.hash);
    return t ? [t] : [];
  });
  const [activeKey, setActiveKey] = useState<string | null>(() => {
    const t = fromHash(location.hash);
    return t ? tabKey(t) : null;
  });

  // open focuses the tab if it is already open, otherwise appends and activates it.
  const open = useCallback((tab: Tab) => {
    const key = tabKey(tab);
    setTabs((ts) => (ts.some((t) => tabKey(t) === key) ? ts : [...ts, tab]));
    setActiveKey(key);
  }, []);

  const close = useCallback(
    (key: string) => {
      const i = tabs.findIndex((t) => tabKey(t) === key);
      if (i < 0) return;
      const rest = tabs.filter((t) => tabKey(t) !== key);
      setTabs(rest);
      if (key === activeKey) {
        const next = rest[Math.min(i, rest.length - 1)];
        setActiveKey(next ? tabKey(next) : null);
      }
    },
    [tabs, activeKey],
  );

  const closeAll = useCallback(() => {
    setTabs([]);
    setActiveKey(null);
  }, []);

  const rename = useCallback((key: string, title: string) => {
    setTabs((ts) => ts.map((t) => (tabKey(t) === key && t.title !== title ? { ...t, title } : t)));
  }, []);

  const active = tabs.find((t) => tabKey(t) === activeKey) ?? null;

  // Active tab -> hash.
  useEffect(() => {
    if (active) {
      const want = toHash(active);
      if (location.hash !== want) location.hash = want;
    } else if (location.hash) {
      history.replaceState(null, '', location.pathname + location.search);
    }
  }, [active]);

  // Hash -> tabs (back/forward, pasted links).
  useEffect(() => {
    const onHash = () => {
      const t = fromHash(location.hash);
      if (t) open(t);
    };
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, [open]);

  return { tabs, active, open, close, closeAll, rename };
}
