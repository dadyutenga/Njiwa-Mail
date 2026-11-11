import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { admin, setCsrf, type AdminUser } from "@/lib/admin-api";
type Auth = {
  user: AdminUser | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
};
const Context = createContext<Auth | null>(null);
export function AuthProvider({ children }: { children: ReactNode }) {
  const queries = useQueryClient();
  const [user, setUser] = useState<AdminUser | null>(null);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    let active = true;
    const expired = () => {
      setCsrf("");
      setUser(null);
      queries.clear();
    };
    window.addEventListener("admin-session-expired", expired);
    admin
      .me()
      .then((u) => {
        if (active) {
          setCsrf(u.csrf_token);
          setUser(u);
        }
      })
      .catch(() => {
        if (active) {
          setCsrf("");
          setUser(null);
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
      window.removeEventListener("admin-session-expired", expired);
    };
  }, [queries]);
  const login = async (email: string, password: string) => {
    const next = await admin.login(email, password);
    queries.clear();
    setCsrf(next.csrf_token);
    setUser(next);
  };
  const logout = async () => {
    try {
      await admin.logout();
    } finally {
      queries.clear();
      setCsrf("");
      setUser(null);
    }
  };
  const refresh = async () => {
    const next = await admin.me();
    setCsrf(next.csrf_token);
    setUser(next);
  };
  return (
    <Context.Provider value={{ user, loading, login, logout, refresh }}>
      {children}
    </Context.Provider>
  );
}
export function useAuth() {
  const value = useContext(Context);
  if (!value) throw new Error("AuthProvider is missing");
  return value;
}
export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const location = useLocation();
  if (loading)
    return (
      <div role="status" className="p-8">
        Checking session…
      </div>
    );
  if (!user) return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  return children;
}
