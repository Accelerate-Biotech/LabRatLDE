import { useState, type FormEvent } from 'react';
import { DEV_TENANT, setSession } from './api';

// Development only: the server trusts these headers when LDE_ENV is dev.
export default function SignIn({ onSignIn }: { onSignIn: (actor: string) => void }) {
  const [tenant, setTenant] = useState(DEV_TENANT);
  const [actor, setActor] = useState('');

  function submit(e: FormEvent) {
    e.preventDefault();
    const t = tenant.trim();
    const a = actor.trim();
    if (!t || !a) return;
    setSession(t, a);
    onSignIn(a);
  }

  return (
    <form className="signin" onSubmit={submit}>
      <h1>Sign in</h1>
      <p className="muted">Development server: your tenant and actor name are sent as headers.</p>
      <label htmlFor="tenant">Tenant</label>
      <input id="tenant" value={tenant} onChange={(e) => setTenant(e.target.value)} required />
      <label htmlFor="actor">Actor</label>
      <input id="actor" value={actor} onChange={(e) => setActor(e.target.value)} autoComplete="username" required />
      <button type="submit">Sign in</button>
    </form>
  );
}
