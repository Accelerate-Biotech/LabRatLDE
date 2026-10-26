import { useEffect, useRef, useState, type KeyboardEvent } from 'react';

export type PaletteItem = { id: string; title: string; hint: string; run: () => void };

// CommandPalette is a modal: type to filter by title, Up/Down to move, Enter to run, Esc to close.
// Focus returns to whatever had it before the palette opened.
export default function CommandPalette({ items, onClose }: { items: PaletteItem[]; onClose: () => void }) {
  const [query, setQuery] = useState('');
  const [sel, setSel] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    input.current?.focus();
    return () => before?.focus();
  }, []);

  const q = query.trim().toLowerCase();
  const matches = items.filter((i) => i.title.toLowerCase().includes(q));
  const at = Math.min(sel, Math.max(matches.length - 1, 0));

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' && matches.length) setSel((at + 1) % matches.length);
    else if (e.key === 'ArrowUp' && matches.length) setSel((at + matches.length - 1) % matches.length);
    else if (e.key === 'Enter') {
      const item = matches[at];
      onClose();
      item?.run();
    } else if (e.key === 'Escape') onClose();
    // Tab: the input is the only focusable element, so keep focus on it.
    else if (e.key !== 'Tab') return;
    e.preventDefault();
  }

  return (
    <div className="palette-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal="true" aria-label="Command palette" className="palette">
        <input
          ref={input}
          type="text"
          role="combobox"
          aria-label="Search commands"
          aria-expanded="true"
          aria-controls="palette-list"
          aria-autocomplete="list"
          aria-activedescendant={matches.length ? `palette-opt-${at}` : undefined}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setSel(0);
          }}
          onKeyDown={onKeyDown}
        />
        <ul role="listbox" id="palette-list" aria-label="Results">
          {matches.map((m, i) => (
            <li
              key={m.id}
              id={`palette-opt-${i}`}
              role="option"
              aria-selected={i === at}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                onClose();
                m.run();
              }}
            >
              <span>{m.title}</span>
              <span className="muted">{m.hint}</span>
            </li>
          ))}
        </ul>
        {matches.length === 0 && <p className="muted">No matches</p>}
      </div>
    </div>
  );
}
