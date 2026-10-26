import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import BottomPanel from './BottomPanel';
import Explorer, { type Loaded } from './Explorer';
import { tabKey, useTabs, type Tab } from './tabs';

// TabContent is the one place that maps a tab kind to its view (tasks 3.3-3.8).
function TabContent({ tab }: { tab: Tab }) {
  switch (tab.kind) {
    case 'experiment':
      return <p>experiment {tab.id}</p>;
    case 'run':
      return <p>run {tab.id}</p>;
    case 'instrument':
      return <p>instrument {tab.id}</p>;
    case 'lineage':
      return <p>lineage {tab.id}</p>;
    case 'audit':
      return <p>audit {tab.id}</p>;
  }
}

export default function Workspace() {
  const { tabs, active, open, close } = useTabs();
  // Read by the instrument view (3.6) and the command palette (3.9).
  const [, setLoaded] = useState<Loaded>({ experiments: [], runs: {}, instruments: [] });
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const tablist = useRef<HTMLDivElement>(null);
  const activeKey = active ? tabKey(active) : null;

  // The roving tab stop follows the active tab.
  useEffect(() => setFocusKey(activeKey), [activeKey]);

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
                  {t.kind === 'audit' ? t.title : `${t.kind[0].toUpperCase()}${t.kind.slice(1)}: ${t.title}`}
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
            <TabContent tab={active} />
          </div>
        ) : (
          <p className="muted tabpanel">Open an experiment, run or instrument from the explorer.</p>
        )}
      </div>
      <BottomPanel active={active} />
    </div>
  );
}
