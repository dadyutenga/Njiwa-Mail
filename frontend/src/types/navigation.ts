import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { UserRole } from "./auth";

export interface RouteConfigItem {
  path: string;
  title: string;
  description?: string;
  icon?: LucideIcon;
  element: ReactNode;
  layout?: "dashboard" | "auth" | "blank";
  roles?: UserRole[];
  inNav?: boolean;
  navSection?: "main" | "management" | "system";
  badge?: string | number;
  exact?: boolean;
  children?: RouteConfigItem[];
}
