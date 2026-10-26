import { useEffect, useState } from 'react';
import { clearSession, meta, storedActor, type Meta } from './api';
import SignIn from './SignIn';

export default function App() {
  const [env, setEnv] = useState<Meta['env'] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [actor, setActor] = useState<string | null>(storedActor());

  useEffect(() => {
    let live = true;
    meta()
      .then((m) => {
        if (live) setEnv(m.env);
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      live = false;
    };
  }, []);

  if (error) return <p role="alert">Could not reach the server: {error}</p>;
  if (!env) return <p>Loading…</p>;
  if (env === 'prod') return <p>Sign-in is not configured yet (SPEC-006).</p>;
  if (!actor) return <SignIn onSignIn={setActor} />;

  return (
    <>
      <header className="topbar">
        <h1>LabRat</h1>
        <span>{actor}</span>
        <button
          type="button"
          onClick={() => {
            clearSession();
            setActor(null);
          }}
        >
          Sign out
        </button>
      </header>
      <div className="workspace" />
    </>
  );
}
