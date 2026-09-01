import { createContext, useContext, useState, useEffect } from "react";
import type { ReactNode } from "react";

type Role = "admin" | "viewer" | null;

interface AuthContextType {
    apiKey: string | null;
    role: Role;
    login: (key: string) => Promise<boolean>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
    const [apiKey, setApiKey] = useState<string | null>(
        () => localStorage.getItem("apiKey")
    );
    const [role, setRole] = useState<Role>(null);

    const checkRole = async (key: string): Promise<Role> => {
        const res = await fetch(`${import.meta.env.VITE_API_URL}/whoami`, {
            headers: { "X-API-Key": key },
        });
        if (!res.ok) return null;
        const data = await res.json();
        return data.role as Role;
    };

    useEffect(() => {
        if (apiKey) {
            checkRole(apiKey).then((r) => {
                if (r) {
                    setRole(r);
                } else {
                    setApiKey(null);
                    localStorage.removeItem("apiKey");
                }
            });
        }
    }, []);

    const login = async (key: string): Promise<boolean> => {
        const r = await checkRole(key);
        if (!r) return false;
        setApiKey(key);
        setRole(r);
        localStorage.setItem("apiKey", key);
        return true;
    };

    const logout = () => {
        setApiKey(null);
        setRole(null);
        localStorage.removeItem("apiKey");
    };

    return (
        <AuthContext.Provider value={{ apiKey, role, login, logout }}>
            {children}
        </AuthContext.Provider>
    );
}

export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) throw new Error("useAuth must be used within AuthProvider");
    return ctx;
}