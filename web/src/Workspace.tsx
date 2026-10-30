import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import AuditView from './AuditView';
import BottomPanel from './BottomPanel';
import CommandPalette, { type PaletteItem } from './CommandPalette';
import Explorer, { type Loaded } from './Explorer';
import ExperimentView from './ExperimentView';
import InboxView from './InboxView';
import InstrumentView from './InstrumentView';
import DelegationsView from './DelegationsView';
import LineageView from './LineageView';
import MeView from './MeView';
import PrincipalsView from './PrincipalsView';
import RunView from './RunView';
import type { Instrument } from './api';
import { SINGLETONS, tabKey, useTabs, type Tab } from './tabs';

// Every view takes the same props: its tab id, plus open and rename from useTabs.
type ViewProps = { id: string; open: (t: Tab) => void; rename: (key: string, title: string) => void };

// TabContent is the one place that maps a tab kind to its view (tasks 3.3-3.8).
function TabContent({ tab, instruments, verifyOnOpen, open, rename }: { tab: Tab; instruments: Instrument[]; verifyOnOpen: number } & Pick<ViewProps, 'open' | 'rename'>) {
  const props: ViewProps = { id: tab.id, open, rename };
  switch (tab.kind) {
    case 'experiment':
      return <ExperimentView {...props} />;
    case 'run':
      return <RunView {...props} />;
    case 'instrument':
      return <InstrumentView {...props} instruments={instruments} />;
    case 'lineage':
      return <LineageView {...props} />;
    case 'audit':
      return <AuditView id={tab.id} verifyOnOpen={verifyOnOpen} />;
    case 'inbox':
      return <InboxView />;
    case 'me':
      return <MeView />;
    case 'principals':
      return <PrincipalsView />;
    case 'delegations':
      return <DelegationsView />;
  }
}

export default function Workspace({ onSignOut }: { onSignOut: () => void }) {
  const { tabs, active, open, close, closeAll, rename } = useTabs();
  // Read by the instrument view and the command palette (3.9).
  const [loaded, setLoaded] = useState<Loaded>({ experiments: [], runs: {}, instruments: [] });
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [verifyCount, setVerifyCount] = useState(0);
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const tablist = useRef<HTMLDivElement>(null);
  const activeKey = active ? tabKey(active) : null;

  // The roving tab stop follows the active tab.
  useEffect(() => setFocusKey(activeKey), [activeKey]);

  // Ctrl+K (Cmd+K on macOS) opens the command palette; while open it stays one dialog.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPaletteOpen(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const auditTab = SINGLETONS.audit!;
  const paletteItems: PaletteItem[] = [
    ...loaded.experiments.map((x) => ({ id: `experiment:${x.id}`, title: x.slug, hint: 'Experiment', run: () => open({ kind: 'experiment', id: x.id, title: x.slug }) })),
    ...Object.values(loaded.runs).flatMap((rs) => rs.map((r) => ({ id: `run:${r.id}`, title: r.label, hint: 'Run', run: () => open({ kind: 'run', id: r.id, title: r.label }) }))),
    ...loaded.instruments.map((i) => ({ id: `instrument:${i.id}`, title: i.name, hint: 'Instrument', run: () => open({ kind: 'instrument', id: i.id, title: i.name }) })),
    { id: 'cmd:audit', title: 'Open audit trail', hint: 'Command', run: () => open(auditTab) },
    {
      id: 'cmd:verify',
      title: 'Verify audit chain',
      hint: 'Command',
      run: () => {
        open(auditTab);
        setVerifyCount((n) => n + 1);
      },
    },
    { id: 'cmd:inbox', title: 'Review inbox', hint: 'Command', run: () => open(SINGLETONS.inbox!) },
    { id: 'cmd:me', title: 'My account', hint: 'Command', run: () => open(SINGLETONS.me!) },
    { id: 'cmd:principals', title: 'Principals', hint: 'Admin', run: () => open(SINGLETONS.principals!) },
    { id: 'cmd:delegations', title: 'Agent delegations', hint: 'Admin', run: () => open(SINGLETONS.delegations!) },
    { id: 'cmd:close-all', title: 'Close all tabs', hint: 'Command', run: closeAll },
    { id: 'cmd:sign-out', title: 'Sign out', hint: 'Command', run: onSignOut },
  ];

  function closeTab(key: string) {
    close(key);
    // Closing removes the focused element: put focus back on the tab strip.
    requestAnimationFrame(() => tablist.current?.querySelector<HTMLElement>('[role="tab"][tabindex="0"]')?.focus());
  }

  function onWorkspaceKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.ctrlKey && e.key.toLowerCase() === 'w' && activeKey) {
      e.preventDefault();
      closeTab(activeKey);
    }
  }

  function onTabKeyDown(e: KeyboardEvent<HTMLButtonElement>, tab: Tab) {
    const els = Array.from(tablist.current!.querySelectorAll<HTMLElement>('[role="tab"]'));
    const i = els.indexOf(e.currentTarget);
    if (e.key === 'ArrowRight') els[(i + 1) % els.length].focus();
    else if (e.key === 'ArrowLeft') els[(i + els.length - 1) % els.length].focus();
    else if (e.key === 'Enter') open(tab);
    else if (e.key === 'Delete') closeTab(tabKey(tab));
    else return;
    e.preventDefault();
  }

  const rovingKey = tabs.some((t) => tabKey(t) === focusKey) ? focusKey : activeKey;

  return (
    <div className="workspace" onKeyDown={onWorkspaceKeyDown}>
      <nav className="explorer" aria-label="Explorer">
        <Explorer open={open} onLoaded={setLoaded} />
      </nav>
      <div className="main">
        <div role="tablist" aria-label="Open items" className="tabstrip" ref={tablist}>
          {tabs.map((t) => {
            const key = tabKey(t);
            return (
              <div key={key} role="presentation" className="tab" data-active={key === activeKey}>
                <button
                  type="button"
                  role="tab"
                  id={`tab-${key}`}
                  aria-selected={key === activeKey}
                  aria-controls="tabpanel"
                  tabIndex={key === rovingKey ? 0 : -1}
                  onClick={() => open(t)}
                  onKeyDown={(e) => onTabKeyDown(e, t)}
                  onFocus={() => setFocusKey(key)}
                >
                  {SINGLETONS[t.kind] ? t.title : `${t.kind[0].toUpperCase()}${t.kind.slice(1)}: ${t.title}`}
                </button>
                <button type="button" className="tab-close" aria-label={`Close ${t.title}`} tabIndex={key === activeKey ? 0 : -1} onClick={() => closeTab(key)}>
                  ×
                </button>
              </div>
            );
          })}
        </div>
        {active ? (
          <div role="tabpanel" id="tabpanel" aria-labelledby={`tab-${tabKey(active)}`} className="tabpanel">
            <TabContent tab={active} instruments={loaded.instruments} verifyOnOpen={verifyCount} open={open} rename={rename} />
          </div>
        ) : (
          <p className="muted tabpanel">Open an experiment, run or instrument from the explorer.</p>
        )}
      </div>
      <BottomPanel active={active} />
      {paletteOpen && <CommandPalette items={paletteItems} onClose={() => setPaletteOpen(false)} />}
    </div>
  );
}
