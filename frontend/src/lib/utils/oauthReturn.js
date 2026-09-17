// Tracks "return to this page" for SCM OAuth flows. Components call
// setOAuthReturnURL() right before bouncing to the SCM provider; the app
// shell calls consumeOAuthReturnURL() after the OAuth callback redirects
// back, so the user lands where the flow started instead of the backend's
// generic redirect target.

const STORAGE_KEY = 'scm_oauth_return';
const MAX_AGE_MS = 15 * 60 * 1000;

/** Remember the current page (or an explicit URL) as the OAuth return target. */
export function setOAuthReturnURL(url = window.location.href) {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ url, ts: Date.now() }));
  } catch {
    // sessionStorage unavailable (private mode) — fall back to the
    // backend's hardcoded redirect target.
  }
}

/**
 * Consume the stored return URL. Only applies when the current page is an
 * OAuth callback landing (oauth=success|error in the query). Returns the
 * destination as a path+query string with the callback params merged in,
 * or null when there is nothing usable.
 */
export function consumeOAuthReturnURL(currentSearch = window.location.search) {
  const params = new URLSearchParams(currentSearch);
  const oauth = params.get('oauth');
  if (oauth !== 'success' && oauth !== 'error') return null;

  let raw;
  try {
    raw = sessionStorage.getItem(STORAGE_KEY);
    sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    return null;
  }
  if (!raw) return null;

  let storedURL;
  try {
    const entry = JSON.parse(raw);
    if (typeof entry?.url !== 'string') return null;
    if (!Number.isFinite(entry?.ts) || Date.now() - entry.ts > MAX_AGE_MS) return null;
    // Same-origin check: only path+search are honored, never other origins.
    storedURL = new URL(entry.url, window.location.origin);
  } catch {
    return null;
  }
  if (storedURL.origin !== window.location.origin) return null;

  // Carry the callback outcome onto the return page so it can show a
  // toast or refresh state.
  const merged = new URLSearchParams(storedURL.search);
  merged.set('oauth', oauth);
  for (const key of ['provider', 'message']) {
    const value = params.get(key);
    if (value !== null) merged.set(key, value);
  }
  return `${storedURL.pathname}?${merged.toString()}`;
}
