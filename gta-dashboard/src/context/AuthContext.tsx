import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import type { ReactNode } from "react";

// "pending" = signed in with Discord, but no role has been granted yet.
export type Role = "admin" | "viewer" | "police" | "judge" | "pending" | null;

const ROLES = ["admin", "viewer", "police", "judge", "pending"];

// What the server said. Only "invalid" means "not signed in / bad key";
// "unreachable" (network error, 5xx, rate limit) must NOT log anyone out.
type IdentityCheck =
  | {
      status: "ok";
      role: Exclude<Role, null>;
      permissions: string[];
      username: string | null;
    }
  | { status: "invalid" }
  | { status: "unreachable" };

interface AuthContextType {
  apiKey: string | null; // null when signed in with Discord
  role: Role;
  username: string | null;
  permissions: string[];
  can: (permission: string) => boolean;
  loading: boolean; // true while the session is being checked on startup
  serverDown: boolean; // backend unreachable and no last-known identity to use
  login: (key: string) => Promise<"ok" | "invalid" | "unreachable">;
  loginWithDiscord: () => void;
  logout: () => void;
  retry: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

// Asks the server who we are: by API key if one is given, otherwise by the
// session cookie (Discord). credentials: "include" sends that cookie.
async function checkIdentity(key: string | null): Promise<IdentityCheck> {
  try {
    const headers: Record<string, string> = {};
    if (key) headers["X-API-Key"] = key;
    const res = await fetch(`${import.meta.env.VITE_API_URL}/whoami`, {
      headers,
      credentials: "include",
    });
    if (res.status === 401 || res.status === 403) return { status: "invalid" };
    if (!res.ok) return { status: "unreachable" };

    const data = await res.json();
    if (ROLES.includes(data.role)) {
      const permissions = Array.isArray(data.permissions)
        ? data.permissions.filter((p: unknown) => typeof p === "string")
        : [];
      return {
        status: "ok",
        role: data.role,
        permissions,
        username: typeof data.username === "string" ? data.username : null,
      };
    }
    return { status: "invalid" };
  } catch {
    return { status: "unreachable" };
  }
}

function readCachedPermissions(): string[] | null {
  try {
    const raw = localStorage.getItem("permissions");
    const parsed = raw ? JSON.parse(raw) : null;
    return Array.isArray(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [role, setRole] = useState<Role>(null);
  const [username, setUsername] = useState<string | null>(null);
  const [permissions, setPermissions] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [serverDown, setServerDown] = useState(false);

  const can = useCallback(
    (permission: string) => permissions.includes(permission),
    [permissions]
  );

  const clearSession = useCallback(() => {
    setApiKey(null);
    setRole(null);
    setUsername(null);
    setPermissions([]);
    setServerDown(false);
    localStorage.removeItem("apiKey");
    localStorage.removeItem("role");
    localStorage.removeItem("permissions");
  }, []);

  const applyIdentity = (
    r: Exclude<Role, null>,
    perms: string[],
    name: string | null
  ) => {
    setRole(r);
    setPermissions(perms);
    setUsername(name);
    setServerDown(false);
    localStorage.setItem("role", r);
    localStorage.setItem("permissions", JSON.stringify(perms));
  };

  const restore = useCallback(async () => {
    const key = localStorage.getItem("apiKey");
    setLoading(true);
    const result = await checkIdentity(key);

    if (result.status === "ok") {
      setApiKey(key);
      applyIdentity(result.role, result.permissions, result.username);
    } else if (result.status === "invalid") {
      clearSession(); // bad key, or simply nobody signed in
    } else {
      // Backend unreachable: reuse the last known role and permissions so a
      // refresh during an outage doesn't throw everyone out. They only decide
      // which buttons show; the backend enforces the rules.
      setApiKey(key);
      const cachedRole = localStorage.getItem("role");
      const cachedPerms = readCachedPermissions();
      if (cachedRole && ROLES.includes(cachedRole) && cachedPerms) {
        setRole(cachedRole as Exclude<Role, null>);
        setPermissions(cachedPerms);
        setServerDown(false);
      } else {
        setServerDown(true);
      }
    }
    setLoading(false);
  }, [clearSession]);

  useEffect(() => {
    restore();
  }, [restore]);

  const login = async (
    key: string
  ): Promise<"ok" | "invalid" | "unreachable"> => {
    const result = await checkIdentity(key);
    if (result.status !== "ok") return result.status;

    localStorage.setItem("apiKey", key);
    setApiKey(key);
    applyIdentity(result.role, result.permissions, result.username);
    return "ok";
  };

  // Full-page redirect: Discord asks for approval, then sends the browser
  // back to the server, which sets the session cookie and returns us here.
  const loginWithDiscord = () => {
    window.location.assign(`${import.meta.env.VITE_API_URL}/auth/discord`);
  };

  const logout = useCallback(() => {
    if (!localStorage.getItem("apiKey")) {
      // Discord session: tell the server to end it
      fetch(`${import.meta.env.VITE_API_URL}/auth/logout`, {
        method: "POST",
        credentials: "include",
        headers: { "X-Requested-With": "dashboard" },
      }).catch(() => {});
    }
    clearSession();
  }, [clearSession]);

  return (
    <AuthContext.Provider
      value={{
        apiKey,
        role,
        username,
        permissions,
        can,
        loading,
        serverDown,
        login,
        loginWithDiscord,
        logout,
        retry: restore,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}
