import MarkdownIt from 'markdown-it';
import { useEffect, useState, type FormEvent } from 'react';
import { addNote, editNote, listNotes, type Note } from './api';

// HTML is disabled, so rendered output cannot contain author-supplied markup.
const md = new MarkdownIt({ html: false, linkify: false });

type Props = { nodeId: string };

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// Notes lists a lineage node's notes, oldest first, with forms to add and edit them.
// A successful note edit dispatches labrat:revised, as the field editors do.
export default function Notes({ nodeId }: Props) {
  const [notes, setNotes] = useState<Note[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [draft, setDraft] = useState('');
  const [preview, setPreview] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);

  const [editId, setEditId] = useState<string | null>(null);
  const [editBody, setEditBody] = useState('');
  const [editReason, setEditReason] = useState('');
  const [editError, setEditError] = useState<string | null>(null);

  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let live = true;
    setLoaded(false);
    setLoadError(null);
    setEditId(null);
    listNotes(nodeId)
      .then((r) => {
        if (!live) return;
        setNotes(r.items);
        setLoaded(true);
      })
      .catch((err: unknown) => {
        if (live) setLoadError(errorMessage(err));
      });
    return () => {
      live = false;
    };
  }, [nodeId]);

  function reload(): Promise<void> {
    return listNotes(nodeId).then((r) => {
      setNotes(r.items);
      setLoaded(true);
    });
  }

  async function add(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSaving(true);
    setAddError(null);
    try {
      await addNote(nodeId, draft);
      setDraft('');
      setPreview(false);
      await reload();
    } catch (err: unknown) {
      setAddError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  function startEdit(n: Note) {
    setEditId(n.id);
    setEditBody(n.body);
    setEditReason('');
    setEditError(null);
  }

  function cancelEdit() {
    setEditId(null);
    setEditError(null);
  }

  async function saveEdit(e: FormEvent<HTMLFormElement>, id: string) {
    e.preventDefault();
    setSaving(true);
    setEditError(null);
    try {
      await editNote(id, editBody, editReason.trim());
      window.dispatchEvent(new CustomEvent('labrat:revised'));
      setEditId(null);
      await reload();
    } catch (err: unknown) {
      setEditError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="notes">
      {loadError && (
        <p role="alert" className="error">
          {loadError}
        </p>
      )}
      {!loaded && !loadError && <p className="muted">Loading notes…</p>}
      {loaded && notes.length === 0 && <p className="muted">No notes yet.</p>}

      {notes.length > 0 && (
        <ol className="note-list">
          {notes.map((n) =>
            editId === n.id ? (
              <li key={n.id} className="note">
                <form className="edit-form" onSubmit={(e) => saveEdit(e, n.id)}>
                  <label>
                    Note
                    <textarea
                      rows={4}
                      value={editBody}
                      onChange={(e) => setEditBody(e.target.value)}
                      disabled={saving}
                      autoFocus
                    />
                  </label>
                  <label>
                    Reason
                    <input
                      value={editReason}
                      onChange={(e) => setEditReason(e.target.value)}
                      disabled={saving}
                      required
                    />
                  </label>
                  {editError && (
                    <p role="alert" className="error">
                      {editError}
                    </p>
                  )}
                  <div className="actions">
                    <button type="submit" disabled={saving || editReason.trim() === '' || editBody.trim() === ''}>
                      Save
                    </button>
                    <button type="button" onClick={cancelEdit} disabled={saving}>
                      Cancel
                    </button>
                  </div>
                </form>
              </li>
            ) : (
              <li key={n.id} className="note">
                <p className="note-meta muted">
                  <span>{n.created_by}</span> · <time dateTime={n.created_at}>{n.created_at}</time>
                  {n.annotation && <span className="note-tag"> Annotation (added after sealing)</span>}
                  {n.rev > 1 && <span className="note-tag"> edited (rev {n.rev})</span>}
                </p>
                <div className="note-body" dangerouslySetInnerHTML={{ __html: md.render(n.body) }} />
                <button type="button" aria-label={`Edit note from ${n.created_by} at ${n.created_at}`} onClick={() => startEdit(n)}>
                  Edit
                </button>
              </li>
            ),
          )}
        </ol>
      )}

      <form className="edit-form" onSubmit={add}>
        {preview ? (
          <div className="note-preview">
            <span className="note-meta muted">Preview</span>
            {draft.trim() === '' ? (
              <p className="muted">Nothing to preview.</p>
            ) : (
              <div className="note-body" dangerouslySetInnerHTML={{ __html: md.render(draft) }} />
            )}
          </div>
        ) : (
          <label>
            Add note
            <textarea rows={4} value={draft} onChange={(e) => setDraft(e.target.value)} disabled={saving} />
          </label>
        )}
        {addError && (
          <p role="alert" className="error">
            {addError}
          </p>
        )}
        <div className="actions">
          <button type="button" aria-pressed={preview} onClick={() => setPreview((p) => !p)}>
            Preview
          </button>
          <button type="submit" disabled={saving || draft.trim() === ''}>
            Save
          </button>
        </div>
      </form>
    </div>
  );
}
