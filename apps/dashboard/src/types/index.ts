import type { ReactNode } from "react";

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
}
