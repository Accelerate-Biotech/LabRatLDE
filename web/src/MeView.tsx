import { useEffect, useState } from 'react';
import { getMe, type Me } from './api';

type Fetched = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; me: Me };

// MeView shows who the server thinks you are, and the roles you hold.
export default function MeView() {
  const [fetched, setFetched] = useState<Fetched>({ status: 'loading' });

  useEffect(() => {
    let live = true;
    getMe()
      .then((me) => {
        if (live) setFetched({ status: 'ready', me });
      })
      .catch((err: unknown) => {
        if (live) setFetched({ status: 'error', message: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      live = false;
    };
  }, []);

  if (fetched.status === 'loading') return <p className="muted">Loading your account…</p>;
  if (fetched.status === 'error') return <p role="alert" className="error">{fetched.message}</p>;
  const { me } = fetched;

  return (
    <article className="view">
      <h1>{me.display_name || me.handle}</h1>
      <table className="data">
        <caption className="sr-only">Your account</caption>
        <tbody>
          <tr>
            <th scope="row">Handle</th>
            <td className="mono">{me.handle}</td>
          </tr>
          <tr>
            <th scope="row">Kind</th>
            <td>{me.kind}</td>
          </tr>
          {me.on_behalf_of && (
            <tr>
              <th scope="row">Acting for</th>
              <td className="mono">{me.on_behalf_of}</td>
            </tr>
          )}
          <tr>
            <th scope="row">Recorded in the audit trail as</th>
            <td className="mono">{me.actor}</td>
          </tr>
          {me.email && (
            <tr>
              <th scope="row">Email</th>
              <td>{me.email}</td>
            </tr>
          )}
          <tr>
            <th scope="row">Signed in with</th>
            <td>{me.auth === 'dev' ? 'Development headers (not for regulated use)' : <span className="mono">{me.issuer}</span>}</td>
          </tr>
        </tbody>
      </table>

      <h2>Roles</h2>
      {me.roles.length === 0 ? (
        <p className="muted">You hold no roles. Ask an admin to grant you one.</p>
      ) : (
        <table className="data">
          <caption className="sr-only">Your roles</caption>
          <thead>
            <tr>
              <th scope="col">Role</th>
              <th scope="col">Scope</th>
              <th scope="col">Granted</th>
              <th scope="col">Reason</th>
            </tr>
          </thead>
          <tbody>
            {me.roles.map((g) => (
              <tr key={g.id}>
                <th scope="row">{g.role}</th>
                <td>{g.scope ?? 'Tenant-wide'}</td>
                <td>{new Date(g.granted_at).toLocaleString()}</td>
                <td>{g.reason}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </article>
  );
}
