import { describe, expect, it } from "vitest";
import {
  beginSilentSso,
  clearSilentSsoState,
  markSignedOut,
  safeReturnTo,
  shouldAttemptSilentSso,
  silentSsoAllowedOnPath,
  silentSsoStartURL,
  type FlagStorage,
} from "./silentSso";
import type { AuthConfig } from "./types";

function memoryStorage(): FlagStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
  };
}

// What a browser in private mode or with site data blocked does.
const brokenStorage = (): FlagStorage => {
  throw new DOMException("The operation is insecure.", "SecurityError");
};

const version = { version: "test", commit: "test", builtAt: "test" };
const autoLogin: AuthConfig = { serviceName: "VisitFlow", companyName: "", localEnabled: true, oidcEnabled: true, oidcAutoLogin: true, version };
const home = { pathname: "/", search: "" };

describe("shouldAttemptSilentSso", () => {
  it("does nothing unless the administrator turned auto-login on", () => {
    const storage = memoryStorage();
    expect(shouldAttemptSilentSso({ ...autoLogin, oidcAutoLogin: false }, home, () => storage)).toBe(false);
    expect(shouldAttemptSilentSso({ ...autoLogin, oidcAutoLogin: undefined }, home, () => storage)).toBe(false);
    expect(shouldAttemptSilentSso({ ...autoLogin, oidcEnabled: false }, home, () => storage)).toBe(false);
    expect(shouldAttemptSilentSso(null, home, () => storage)).toBe(false);
    expect(shouldAttemptSilentSso(autoLogin, home, () => storage)).toBe(true);
  });

  it("tries once per tab session and not again after the attempt", () => {
    const storage = memoryStorage();
    expect(shouldAttemptSilentSso(autoLogin, home, () => storage)).toBe(true);
    const visited: string[] = [];
    beginSilentSso("/visits", () => storage, (url) => visited.push(url));
    expect(visited).toEqual(["/api/v1/auth/oidc/start?prompt=none&returnTo=%2Fvisits"]);
    // A reload after the refusal must not start another round trip.
    expect(shouldAttemptSilentSso(autoLogin, home, () => storage)).toBe(false);
    // A new tab has empty sessionStorage and tries again.
    expect(shouldAttemptSilentSso(autoLogin, home, memoryStorage)).toBe(true);
  });

  it("does not retry on the address the callback marks after a refusal", () => {
    const storage = memoryStorage();
    expect(shouldAttemptSilentSso(autoLogin, { pathname: "/login", search: "?sso=none" }, () => storage)).toBe(false);
    // Even away from the login screen the marker alone is enough, in case
    // storage was cleared in between.
    expect(shouldAttemptSilentSso(autoLogin, { pathname: "/", search: "?sso=none" }, () => storage)).toBe(false);
    expect(shouldAttemptSilentSso(autoLogin, { pathname: "/", search: "?sso=error" }, () => storage)).toBe(false);
    expect(shouldAttemptSilentSso(autoLogin, { pathname: "/", search: "?tab=today" }, () => storage)).toBe(true);
  });

  it("stays quiet after a deliberate sign-out until a session exists again", () => {
    const storage = memoryStorage();
    markSignedOut(() => storage);
    expect(shouldAttemptSilentSso(autoLogin, home, () => storage)).toBe(false);
    clearSilentSsoState(() => storage);
    expect(shouldAttemptSilentSso(autoLogin, home, () => storage)).toBe(true);
  });

  it("treats unreadable storage as already attempted", () => {
    expect(shouldAttemptSilentSso(autoLogin, home, brokenStorage)).toBe(false);
    // Writing must not throw either, or the sign-out button would break.
    expect(() => markSignedOut(brokenStorage)).not.toThrow();
    expect(() => clearSilentSsoState(brokenStorage)).not.toThrow();
    const visited: string[] = [];
    expect(() => beginSilentSso("/", brokenStorage, (url) => visited.push(url))).not.toThrow();
    expect(visited).toHaveLength(1);
  });

  it("never starts from the login, callback or non-page paths", () => {
    const storage = memoryStorage();
    for (const pathname of ["/login", "/login/", "/api/v1/auth/oidc/callback", "/api/v1/auth/oidc/start", "/mcp", "/healthz", "/metrics"]) {
      expect(silentSsoAllowedOnPath(pathname), pathname).toBe(false);
      expect(shouldAttemptSilentSso(autoLogin, { pathname, search: "" }, () => storage), pathname).toBe(false);
    }
    for (const pathname of ["/", "/visits", "/visits/new", "/admin/settings", "/lobby", "/approvals"]) {
      expect(silentSsoAllowedOnPath(pathname), pathname).toBe(true);
    }
  });
});

describe("return path", () => {
  it("carries a deep link through the provider and back", () => {
    expect(silentSsoStartURL("/admin/visits?status=PENDING#top")).toBe(
      "/api/v1/auth/oidc/start?prompt=none&returnTo=%2Fadmin%2Fvisits%3Fstatus%3DPENDING%23top",
    );
  });

  it("only accepts a same-origin path", () => {
    expect(safeReturnTo("/visits")).toBe("/visits");
    expect(safeReturnTo("//evil.example/")).toBe("/");
    expect(safeReturnTo("/\\evil.example/")).toBe("/");
    expect(safeReturnTo("https://evil.example/")).toBe("/");
    expect(safeReturnTo("visits")).toBe("/");
    expect(safeReturnTo("")).toBe("/");
  });
});
