import { useState, type FormEvent } from 'react';
import { DEV_TENANT, setSession, type Meta } from './api';
import { beginSignIn } from './auth';

type Props = { meta: Meta; message: string | null; onDevSignIn: (actor: string) => void };

// In prod, sign-in goes to the organisation's identity provider. In dev the
// server trusts the tenant and actor headers this form stores.
export default function SignIn({ meta, message, onDevSignIn }: Props) {
  return (
    <div className="signin">
      <h1>Sign in</h1>
      {message && <p role="status">{message}</p>}
      {meta.env === 'prod' ? <OIDCSignIn meta={meta} /> : <DevSignIn onSignIn={onDevSignIn} />}
    </div>
  );
}

function OIDCSignIn({ meta }: { meta: Meta }) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const oidc = meta.oidc;
  if (!oidc?.client_id) {
    return <p role="alert">Sign-in is not configured: the server needs LDE_OIDC_UI_CLIENT_ID.</p>;
  }

  function go() {
    setBusy(true);
    setError(null);
    beginSignIn(oidc!).catch((err: unknown) => {
      setBusy(false);
      setError(err instanceof Error ? err.message : String(err));
    });
  }

  return (
    <>
      <p className="muted">You'll sign in with your organisation's account.</p>
      <button type="button" onClick={go} disabled={busy}>
        {busy ? 'Redirecting…' : 'Sign in with your organisation'}
      </button>
      {error && <p role="alert">{error}</p>}
    </>
  );
}

function DevSignIn({ onSignIn }: { onSignIn: (actor: string) => void }) {
  const [tenant, setTenant] = useState(DEV_TENANT);
  const [actor, setActor] = useState('');

  function submit(e: FormEvent) {
    e.preventDefault();
    const t = tenant.trim();
    const a = actor.trim().toLowerCase();
    if (!t || !a) return;
    setSession(t, a);
    onSignIn(a);
  }

  return (
    <form onSubmit={submit}>
      <p className="muted">Development server: your tenant and handle are sent as headers. A new handle gets every role.</p>
      <label htmlFor="tenant">Tenant</label>
      <input id="tenant" value={tenant} onChange={(e) => setTenant(e.target.value)} required />
      <label htmlFor="actor">Handle</label>
      <input id="actor" value={actor} onChange={(e) => setActor(e.target.value)} autoComplete="username" required />
      <button type="submit">Sign in</button>
    </form>
  );
}
