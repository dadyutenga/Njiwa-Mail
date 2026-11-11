import { createContext, useContext, useState, type ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { tokenStorage } from "@/lib/token-storage";
type User = { name: string; role: "member" | "admin" };
type Auth = { user: User | null; login: (name: string) => void; logout: () => void };
const Context = createContext<Auth | null>(null);
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(() => {
    try {
      const value = localStorage.getItem("template-user");
      if (!value) return null;
      const parsed: unknown = JSON.parse(value);
      if (
        typeof parsed !== "object" ||
        parsed === null ||
        !("name" in parsed) ||
        !("role" in parsed)
      )
        return null;
      if (typeof parsed.name !== "string" || (parsed.role !== "member" && parsed.role !== "admin"))
        return null;
      return { name: parsed.name, role: parsed.role };
    } catch {
      return null;
    }
  });
  const login = (name: string) => {
    const next: User = { name, role: "member" };
    localStorage.setItem("template-user", JSON.stringify(next));
    setUser(next);
  };
  const logout = () => {
    localStorage.removeItem("template-user");
    tokenStorage.clear();
    setUser(null);
  };
  return <Context.Provider value={{ user, login, logout }}>{children}</Context.Provider>;
}
export function useAuth() {
  const value = useContext(Context);
  if (!value) throw new Error("AuthProvider is missing");
  return value;
}
export function ProtectedRoute({ children, role }: { children: ReactNode; role?: User["role"] }) {
  const { user } = useAuth();
  const location = useLocation();
  if (!user) return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  if (role && user.role !== role) return <Navigate to="/403" replace />;
  return children;
}
