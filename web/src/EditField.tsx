import { useEffect, useRef, useState, type FormEvent } from 'react';

type Props = {
  label: string;
  value: string;
  multiline?: boolean;
  json?: boolean;
  changed: boolean;
  onSave: (value: string, reason: string) => Promise<void>;
};

// EditField shows a value with an Edit control. Saving needs a reason. A "changed" button
// opens the bottom panel's Revisions tab. A successful save dispatches labrat:revised.
export default function EditField({ label, value, multiline, json, changed, onSave }: Props) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const editButton = useRef<HTMLButtonElement>(null);
  const wasEditing = useRef(false);

  // Leaving edit mode unmounts the form, so return focus to the Edit button.
  useEffect(() => {
    if (!editing && wasEditing.current) editButton.current?.focus();
    wasEditing.current = editing;
  }, [editing]);

  function startEdit() {
    setDraft(value);
    setReason('');
    setError(null);
    setEditing(true);
  }

  function cancel() {
    setError(null);
    setEditing(false);
  }

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (json) {
      let parsed: unknown;
      try {
        parsed = JSON.parse(draft);
      } catch {
        setError('Value must be valid JSON.');
        return;
      }
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        setError('Value must be a JSON object.');
        return;
      }
    }
    setSaving(true);
    setError(null);
    try {
      await onSave(draft, reason.trim());
      setEditing(false);
      window.dispatchEvent(new CustomEvent('labrat:revised'));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="edit-field">
        <span className={json || multiline ? 'edit-value mono' : 'edit-value'}>{value}</span>
        <button type="button" ref={editButton} aria-label={`Edit ${label}`} onClick={startEdit}>
          Edit
        </button>
        {changed && (
          <button
            type="button"
            className="changed"
            aria-label={`${label} changed, show revisions`}
            onClick={() => window.dispatchEvent(new CustomEvent('labrat:show-revisions'))}
          >
            changed
          </button>
        )}
      </div>
    );
  }

  return (
    <form className="edit-form" onSubmit={save}>
      <label>
        {label}
        {json || multiline ? (
          <textarea
            className={json ? 'mono' : undefined}
            rows={json ? 8 : 4}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            disabled={saving}
            autoFocus
          />
        ) : (
          <input value={draft} onChange={(e) => setDraft(e.target.value)} disabled={saving} autoFocus />
        )}
      </label>
      <label>
        Reason
        <input value={reason} onChange={(e) => setReason(e.target.value)} disabled={saving} required />
      </label>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      <div className="actions">
        <button type="submit" disabled={saving || reason.trim() === ''}>
          Save
        </button>
        <button type="button" onClick={cancel} disabled={saving}>
          Cancel
        </button>
      </div>
    </form>
  );
}
