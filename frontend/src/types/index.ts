import type { ReactNode } from "react";

export type Role = "admin" | "member";

export interface User {
  name: string;
  role: Role;
  email?: string;
}

export interface AuthContextType {
  user: User | null;
  login: (name: string, role?: Role) => void;
  logout: () => void;
}

export type Theme = "light" | "dark" | "system";

export type Locale = "en" | "sw";

export interface RecordItem {
  id: string;
  name: string;
  status: "Active" | "Draft" | "Archived";
  owner: string;
  createdAt?: string;
  notes?: string;
}

export interface Column<T> {
  key: keyof T;
  label: string;
  render?: (row: T) => ReactNode;
}

export interface ReportItem {
  date: string;
  name: string;
  value: number;
}

export interface ActivityItem {
  id: string;
  label: string;
  time: string;
}

export interface NavRoute {
  path: string;
  label: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
  component: React.ComponentType;
  navigation: boolean;
  requiredRole?: Role;
}
