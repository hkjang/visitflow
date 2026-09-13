import type { AuthConfig } from "./types";

// Silent SSO: when Keycloak already holds a session, the browser is sent through
// the provider with prompt=none before the login screen is ever drawn. The
// provider never renders anything for such a request — it either answers with a
// code straight away or comes back with login_required, which is an ordinary
// answer and not a failure.
//
// Everything in this module exists to make sure that answer is never retried:
// a retry would bounce the browser between the provider and the app forever.

// sessionStorage rather than localStorage: a fresh tab tries again, a reload
// after a refusal does not.
const ATTEMPTED_KEY = "visitflow.sso.silentAttempted";
const SIGNED_OUT_KEY = "visitflow.sso.signedOut";

/** The subset of Storage the rules need, so they can be tested without a DOM. */
export interface FlagStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

function defaultStorage(): FlagStorage {
  return window.sessionStorage;
}

function readFlag(key: string, storage: () => FlagStorage): boolean {
  try {
    return storage().getItem(key) === "true";
  } catch {
    // Private modes and blocked site data throw here. Reading that as "not yet
    // attempted" would start the loop, so the failure goes the blocking way.
    return true;
  }
}

function writeFlag(key: string, value: boolean, storage: () => FlagStorage) {
  try {
    if (value) storage().setItem(key, "true");
    else storage().removeItem(key);
  } catch {
    /* nothing to do; readFlag already fails closed */
  }
}

/** Records a deliberate sign-out, which suppresses silent sign-in afterwards. */
export function markSignedOut(storage: () => FlagStorage = defaultStorage) {
  writeFlag(SIGNED_OUT_KEY, true, storage);
  writeFlag(ATTEMPTED_KEY, true, storage);
}

/** Lifts the suppression once a session exists again. */
export function clearSilentSsoState(storage: () => FlagStorage = defaultStorage) {
  writeFlag(SIGNED_OUT_KEY, false, storage);
  writeFlag(ATTEMPTED_KEY, false, storage);
}

/** Paths that never start a silent attempt: the login screen itself, the SSO
 * callback and error landings, and everything that is not a browser page. */
const EXCLUDED_PREFIXES = ["/login", "/api", "/mcp", "/healthz", "/metrics"];

export function silentSsoAllowedOnPath(pathname: string): boolean {
  return !EXCLUDED_PREFIXES.some((prefix) => pathname === prefix || pathname.startsWith(prefix + "/"));
}

export interface SilentSsoLocation {
  pathname: string;
  search: string;
}

/**
 * Decides whether to try signing in without showing a login screen. It must
 * answer true at most once per tab session, and never right after a sign-out,
 * never on a page that carries the refusal marker, and never when the
 * administrator has not turned auto-login on.
 */
export function shouldAttemptSilentSso(
  config: AuthConfig | null | undefined,
  location: SilentSsoLocation,
  storage: () => FlagStorage = defaultStorage,
): boolean {
  if (!config?.oidcEnabled || !config.oidcAutoLogin) return false;
  if (!silentSsoAllowedOnPath(location.pathname)) return false;
  // The callback appends this marker when the provider had no session, so the
  // refusal is remembered even if sessionStorage was cleared in between.
  const sso = new URLSearchParams(location.search).get("sso");
  if (sso === "none" || sso === "error") return false;
  if (readFlag(SIGNED_OUT_KEY, storage)) return false;
  if (readFlag(ATTEMPTED_KEY, storage)) return false;
  return true;
}

/** Only a same-origin path may be carried through the provider and back.
 * Browsers read "/\host" as "//host", hence the backslash check. */
export function safeReturnTo(value: string): string {
  return value.startsWith("/") && !value.startsWith("//") && !value.includes("\\") ? value : "/";
}

/** Builds the address that starts a silent attempt for the given return path. */
export function silentSsoStartURL(returnTo: string): string {
  return `/api/v1/auth/oidc/start?prompt=none&returnTo=${encodeURIComponent(safeReturnTo(returnTo))}`;
}

/**
 * Sends the browser to the provider for a silent attempt. The attempt is
 * recorded before navigating, so a page that loads again before the provider
 * answers cannot start a second one.
 */
export function beginSilentSso(
  returnTo: string,
  storage: () => FlagStorage = defaultStorage,
  navigate: (url: string) => void = (url) => window.location.assign(url),
) {
  writeFlag(ATTEMPTED_KEY, true, storage);
  navigate(silentSsoStartURL(returnTo));
}
