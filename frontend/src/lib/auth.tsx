"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { apiRequest } from "@/lib/api";
import type { User } from "@/lib/types";

const TOKEN_KEY = "inspector_access_token";
type AuthContextValue = { token: string | null; user: User | null; ready: boolean; signIn: (login: string, password: string) => Promise<void>; signOut: () => void };
const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [ready, setReady] = useState(false);

  const signOut = useCallback(() => {
    sessionStorage.removeItem(TOKEN_KEY);
    setToken(null);
    setUser(null);
    setReady(true);
  }, []);

  useEffect(() => {
    const stored = sessionStorage.getItem(TOKEN_KEY);
    if (!stored) { setReady(true); return; }
    setToken(stored);
    apiRequest<User>("/auth/me", stored).then(setUser).catch(() => {
      sessionStorage.removeItem(TOKEN_KEY);
      setToken(null);
      setUser(null);
    }).finally(() => setReady(true));
  }, []);

  useEffect(() => {
    window.addEventListener("inspector:unauthorized", signOut);
    return () => window.removeEventListener("inspector:unauthorized", signOut);
  }, [signOut]);

  const signIn = useCallback(async (login: string, password: string) => {
    const result = await apiRequest<{ access_token: string }>("/auth/login", null, { method: "POST", body: JSON.stringify({ login, password }) });
    sessionStorage.setItem(TOKEN_KEY, result.access_token);
    setToken(result.access_token);
    const currentUser = await apiRequest<User>("/auth/me", result.access_token);
    setUser(currentUser);
    setReady(true);
  }, []);

  const value = useMemo(() => ({ token, user, ready, signIn, signOut }), [token, user, ready, signIn, signOut]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error("useAuth must be used inside AuthProvider");
  return context;
}
