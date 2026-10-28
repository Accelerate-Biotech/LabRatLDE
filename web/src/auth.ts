// OIDC sign-in for the UI: authorization code + PKCE, with tokens in memory
// only (SPEC-006 design §3). Only the PKCE verifier and state cross the
// redirect, in sessionStorage, and they are removed on return.

import type { OIDCSettings } from './api';

const PENDING_KEY = 'labrat.oidc.pending';

type Discovery = { authorization_endpoint: string; token_endpoint: string; end_session_endpoint?: string };

type Pending = { verifier: string; state: string; hash: string };

export type Tokens = { accessToken: string; idToken?: string };

let discovery: Promise<Discovery> | null = null;

function discover(issuer: string): Promise<Discovery> {
  discovery ??= fetch(`${issuer.replace(/\/$/, '')}/.well-known/openid-configuration`).then(async (res) => {
    if (!res.ok) {
      discovery = null;
      throw new Error(`OIDC discovery failed (${res.status})`);
    }
    return res.json() as Promise<Discovery>;
  });
  return discovery;
}

function base64url(bytes: Uint8Array): string {
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function randomString(): string {
  const b = new Uint8Array(32);
  crypto.getRandomValues(b);
  return base64url(b);
}

async function s256(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(digest));
}

// The IdP client must list this exact redirect URI.
function redirectUri(): string {
  return `${location.origin}/`;
}

// beginSignIn sends the browser to the IdP. The open tab (location.hash) is
// restored on return.
export async function beginSignIn(oidc: OIDCSettings): Promise<void> {
  const d = await discover(oidc.issuer);
  const pending: Pending = { verifier: randomString(), state: randomString(), hash: location.hash };
  try {
    sessionStorage.setItem(PENDING_KEY, JSON.stringify(pending));
  } catch {
    throw new Error('Sign-in needs session storage, which this browser has disabled.');
  }
  const url = new URL(d.authorization_endpoint);
  url.search = new URLSearchParams({
    response_type: 'code',
    client_id: oidc.client_id,
    redirect_uri: redirectUri(),
    scope: oidc.scopes,
    state: pending.state,
    code_challenge: await s256(pending.verifier),
    code_challenge_method: 'S256',
  }).toString();
  location.assign(url.toString());
}

function takePending(): Pending | null {
  try {
    const raw = sessionStorage.getItem(PENDING_KEY);
    sessionStorage.removeItem(PENDING_KEY);
    return raw ? (JSON.parse(raw) as Pending) : null;
  } catch {
    return null;
  }
}

// completeSignIn finishes a return from the IdP: it checks state, swaps the
// code for tokens and cleans the URL. It returns null when the page wasn't
// opened by a sign-in redirect.
export async function completeSignIn(oidc: OIDCSettings): Promise<Tokens | null> {
  const params = new URLSearchParams(location.search);
  const code = params.get('code');
  const error = params.get('error');
  if (!code && !error) return null;
  const pending = takePending();
  history.replaceState(null, '', location.pathname + (pending?.hash ?? ''));
  if (error) throw new Error(`Sign-in failed: ${params.get('error_description') ?? error}`);
  if (!pending || pending.state !== params.get('state')) throw new Error('Sign-in failed: the response did not match this browser’s request. Try again.');

  const d = await discover(oidc.issuer);
  const res = await fetch(d.token_endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'authorization_code',
      code: code!,
      redirect_uri: redirectUri(),
      client_id: oidc.client_id,
      code_verifier: pending.verifier,
    }),
  });
  if (!res.ok) throw new Error(`Sign-in failed: the identity provider returned ${res.status}`);
  const body = (await res.json()) as { access_token?: string; id_token?: string };
  if (!body.access_token) throw new Error('Sign-in failed: no access token was returned');
  return { accessToken: body.access_token, idToken: body.id_token };
}

// watchIdle calls onIdle after seconds without a pointer, key or wheel event
// (SPEC-006-R11). It returns a function that stops watching.
export function watchIdle(seconds: number, onIdle: () => void): () => void {
  const events = ['pointerdown', 'keydown', 'wheel'] as const;
  let timer = window.setTimeout(onIdle, seconds * 1000);
  const reset = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(onIdle, seconds * 1000);
  };
  for (const e of events) window.addEventListener(e, reset, { capture: true, passive: true });
  return () => {
    window.clearTimeout(timer);
    for (const e of events) window.removeEventListener(e, reset, { capture: true });
  };
}

// endSessionUrl is where to send the browser to end the IdP session too, or
// null when the IdP doesn't advertise one.
export async function endSessionUrl(oidc: OIDCSettings, idToken?: string): Promise<string | null> {
  const d = await discover(oidc.issuer);
  if (!d.end_session_endpoint) return null;
  const url = new URL(d.end_session_endpoint);
  const q = new URLSearchParams({ client_id: oidc.client_id, post_logout_redirect_uri: redirectUri() });
  if (idToken) q.set('id_token_hint', idToken);
  url.search = q.toString();
  return url.toString();
}
