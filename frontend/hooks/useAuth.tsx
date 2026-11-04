"use client";

import { createContext, useCallback, useContext, useEffect, useState, ReactNode } from "react";
import { api } from "@/lib/api";

export type User = {
  id: string;
  name: string;
  email: string;
  role: string;
  business_id: string;
  active: boolean;
};

type AuthContextType = {
  user: User | null;
  loading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
};

const AuthContext = createContext<AuthContextType | undefined>(undefined);

/** Loads the signed-in user and keeps the role fresh while the tab is active. */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchUser = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const res = await api.get("/auth/me");
      setUser(res.data.data);
    } catch (e: unknown) {
      const err = e as { response?: { data?: { error?: string } } };
      setError(err.response?.data?.error || "Failed to fetch user");
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, []);

  // Silent refresh: role changes made by another admin should reach a mounted
  // dashboard without flipping `loading` and remounting the app shell.
  const refreshUser = useCallback(async () => {
    try {
      const res = await api.get("/auth/me");
      setUser(res.data.data);
      setError(null);
    } catch {
      // Keep the current user; the API interceptor handles expired sessions.
    }
  }, []);

  useEffect(() => {
    void fetchUser();
  }, [fetchUser]);

  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState !== "hidden") void refreshUser();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [refreshUser]);

  return (
    <AuthContext.Provider value={{ user, loading, error, refetch: fetchUser }}>
      {children}
    </AuthContext.Provider>
  );
}

/** Returns the auth context, failing loudly when used outside the provider. */
export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}