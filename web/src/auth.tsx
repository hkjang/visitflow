import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { api, postJSON, setCSRF } from "./api";
import { clearSilentSsoState, markSignedOut } from "./silentSso";
import type { AuthConfig, User, VersionInfo } from "./types";

interface AuthState {
  user: User | null;
  phoneMasked: string;
  config: AuthConfig | null;
  version: VersionInfo | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  reload: () => Promise<void>;
}
const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null),
    [phoneMasked, setPhoneMasked] = useState(""),
    [config, setConfig] = useState<AuthConfig | null>(null),
    [version, setVersion] = useState<VersionInfo | null>(null),
    [loading, setLoading] = useState(true);
  const reload = useCallback(async () => {
    try {
      const c = await api<AuthConfig>("/api/v1/auth/config");
      setConfig(c);
      setVersion(c.version);
      try {
        const me = await api<{
          user: User;
          phoneMasked?: string;
          csrfToken: string;
          version: VersionInfo;
        }>("/api/v1/auth/me");
        setUser(me.user);
        setPhoneMasked(me.phoneMasked ?? "");
        setVersion(me.version);
        setCSRF(me.csrfToken);
        // A live session lifts the silent-SSO suppression left by a sign-out.
        clearSilentSsoState();
      } catch {
        setUser(null);
        setCSRF("");
      }
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);
  const login = useCallback(
    async (username: string, password: string) => {
      await postJSON<User>("/api/v1/auth/login", { username, password });
      await reload();
    },
    [reload],
  );
  const logout = useCallback(async () => {
    // Recorded before the session goes away: signing the user straight back in
    // silently would make the sign-out look broken.
    markSignedOut();
    try {
      await api<void>("/api/v1/auth/logout", { method: "POST" });
    } finally {
      setUser(null);
      setCSRF("");
    }
  }, []);
  const value = useMemo(
    () => ({ user, phoneMasked, config, version, loading, login, logout, reload }),
    [user, phoneMasked, config, version, loading, login, logout, reload],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("AuthProvider missing");
  return value;
}
