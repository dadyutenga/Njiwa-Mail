import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { User, UserRole, LoginCredentials, RegisterCredentials } from "@/types/auth";
import { apiClient } from "@/lib/api";
import { setCsrfToken } from "@/lib/csrf";

interface AuthContextType {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (credentials: LoginCredentials) => Promise<void>;
  register: (data: RegisterCredentials) => Promise<void>;
  logout: () => Promise<void>;
  updateUser: (updates: Partial<User>) => void;
  switchRole: (role: UserRole) => void;
}

interface SessionUser {
  id: string;
  email: string;
  role: UserRole;
  csrf_token: string;
  expires_at: string;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

function asUser(session: SessionUser): User {
  setCsrfToken(session.csrf_token);
  return {
    id: session.id,
    name: session.email,
    email: session.email,
    role: session.role,
    createdAt: session.expires_at,
  };
}

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const response = await apiClient.get<SessionUser>("/auth/me");
        if (!cancelled) {
          setUser(asUser(response.data));
          setToken("session");
        }
      } catch {
        if (!cancelled) {
          setCsrfToken("");
          setUser(null);
          setToken(null);
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const login = async (credentials: LoginCredentials): Promise<void> => {
    setIsLoading(true);
    try {
      const response = await apiClient.post<SessionUser>("/auth/login", {
        email: credentials.email,
        password: credentials.password,
      });
      setUser(asUser(response.data));
      setToken("session");
    } finally {
      setIsLoading(false);
    }
  };

  const register = async (_data: RegisterCredentials): Promise<void> => {
    throw new Error("There is no public signup. An owner creates accounts.");
  };

  const logout = async (): Promise<void> => {
    try {
      await apiClient.post("/auth/logout");
    } finally {
      setCsrfToken("");
      setUser(null);
      setToken(null);
    }
  };

  const updateUser = (updates: Partial<User>): void => {
    setUser((current) => (current ? { ...current, ...updates } : current));
  };

  const switchRole = (_role: UserRole): void => {
    // Roles come from the server. The UI does not preview another role.
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        token,
        isAuthenticated: !!user && !!token,
        isLoading,
        login,
        register,
        logout,
        updateUser,
        switchRole,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return context;
};
