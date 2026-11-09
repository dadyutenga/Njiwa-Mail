import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { User, UserRole, LoginCredentials, RegisterCredentials } from "@/types/auth";
import { apiClient } from "@/lib/api";
import type { ConsoleMe } from "@/types/mail";

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

const AuthContext = createContext<AuthContextType | undefined>(undefined);

function operatorUser(me: ConsoleMe): User {
  return {
    id: me.id,
    name: me.name,
    email: "operator@ditronics.mail",
    role: "admin",
    createdAt: new Date().toISOString(),
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
        const response = await apiClient.get<ConsoleMe>("/me");
        if (!cancelled) {
          setUser(operatorUser(response.data));
          setToken("session");
        }
      } catch {
        if (!cancelled) {
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
      await apiClient.post("/login", { password: credentials.password });
      const response = await apiClient.get<ConsoleMe>("/me");
      setUser(operatorUser(response.data));
      setToken("session");
    } finally {
      setIsLoading(false);
    }
  };

  const register = async (_data: RegisterCredentials): Promise<void> => {
    throw new Error("Operator access is configured on the API. There is no self-service signup.");
  };

  const logout = async (): Promise<void> => {
    try {
      await apiClient.post("/logout");
    } finally {
      setUser(null);
      setToken(null);
    }
  };

  const updateUser = (updates: Partial<User>): void => {
    setUser((current) => (current ? { ...current, ...updates } : current));
  };

  const switchRole = (_role: UserRole): void => {
    // The console has a single operator session. Role preview is not a backend role.
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
