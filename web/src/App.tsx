import { useCallback, useEffect, useState } from 'react';
import { clearSession, getMe, meta as loadMeta, setAccessToken, setUnauthorizedHandler, storedActor, type Meta } from './api';
import { completeSignIn, endSessionUrl } from './auth';
import SignIn from './SignIn';
import Workspace from './Workspace';

// A signed-in session: who to show in the header, and (in prod) the ID token
// the IdP's sign-out wants.
type Session = { handle: string; idToken?: string };

export default function App() {
  const [meta, setMeta] = useState<Meta | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [session, setSession] = useState<Session | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  // signOut discards the token (or the dev session) and returns to sign-in.
  const signOut = useCallback((why: string | null) => {
    setAccessToken(null);
    clearSession();
    setSession(null);
    setMessage(why);
  }, []);

  useEffect(() => {
    let live = true;
    (async () => {
      const m = await loadMeta();
      if (!live) return;
      setMeta(m);
      if (m.env === 'dev') {
        const actor = storedActor();
        if (actor) setSession({ handle: actor });
        return;
      }
      if (!m.oidc) return;
      try {
        const tokens = await completeSignIn(m.oidc);
        if (!tokens || !live) return;
        setAccessToken(tokens.accessToken);
        const me = await getMe();
        if (live) setSession({ handle: me.on_behalf_of ? me.actor : me.handle, idToken: tokens.idToken });
      } catch (err: unknown) {
        setAccessToken(null);
        if (live) setMessage(err instanceof Error ? err.message : String(err));
      }
    })().catch((err: unknown) => {
      if (live) setError(err instanceof Error ? err.message : String(err));
    });
    return () => {
      live = false;
    };
  }, []);

  useEffect(() => {
    setUnauthorizedHandler(() => signOut('Your session has ended. Sign in again.'));
  }, [signOut]);

  if (error) return <p role="alert">Could not reach the server: {error}</p>;
  if (!meta) return <p>Loading…</p>;
  if (!session) return <SignIn meta={meta} message={message} onDevSignIn={(handle) => setSession({ handle })} />;

  // In prod, also end the IdP session, so a shared bench computer doesn't
  // sign the next person straight back in.
  function userSignOut() {
    const idToken = session?.idToken;
    signOut(null);
    if (meta?.env === 'prod' && meta.oidc) {
      endSessionUrl(meta.oidc, idToken)
        .then((url) => {
          if (url) location.assign(url);
        })
        .catch(() => {});
    }
  }

  return (
    <>
      <header className="topbar">
        <h1>LabRat</h1>
        <a href="#/me/me" title="My account">
          {session.handle}
        </a>
        <kbd className="muted">Ctrl+K</kbd>
        <button type="button" onClick={userSignOut}>
          Sign out
        </button>
      </header>
      <Workspace onSignOut={userSignOut} />
    </>
  );
}
