import React, { lazy } from "react";
import {
  LayoutDashboard,
  Mail,
  BarChart3,
  FolderKanban,
  User,
  Settings,
  HelpCircle,
} from "lucide-react";
import type { RouteConfigItem } from "@/types/navigation";

// Lazy-loaded page components for automatic code-splitting
const DashboardPage = lazy(() => import("@/pages/DashboardPage"));
const ListPage = lazy(() => import("@/pages/ListPage"));
const DetailPage = lazy(() => import("@/pages/DetailPage"));
const ProjectsPage = lazy(() => import("@/pages/ProjectsPage"));
const ReportsPage = lazy(() => import("@/pages/ReportsPage"));
const ProfilePage = lazy(() => import("@/pages/ProfilePage"));
const SettingsPage = lazy(() => import("@/pages/SettingsPage"));
const ExamplePage = lazy(() => import("@/pages/ExamplePage"));

const LoginPage = lazy(() => import("@/pages/LoginPage"));
const RegisterPage = lazy(() => import("@/pages/RegisterPage"));
const ForgotPasswordPage = lazy(() => import("@/pages/ForgotPasswordPage"));
const ResetPasswordPage = lazy(() => import("@/pages/ResetPasswordPage"));

const LandingPage = lazy(() => import("@/pages/LandingPage"));
const NotFoundPage = lazy(() => import("@/pages/NotFoundPage"));
const ForbiddenPage = lazy(() => import("@/pages/ForbiddenPage"));
const ServerErrorPage = lazy(() => import("@/pages/ServerErrorPage"));

/**
 * THE UNIFIED NAVIGATION & ROUTE CONFIGURATION ARRAY
 *
 * To add a new page to the app:
 * 1. Create your page component in `src/pages/`
 * 2. Add an entry to the array below!
 *
 * The router, desktop sidebar, and mobile drawer all derive their paths,
 * labels, icons, permissions, and layout from this single array.
 */
export const routesConfig: RouteConfigItem[] = [
  // --- Public Marketing & Auth ---
  {
    path: "/",
    title: "Home",
    element: <LandingPage />,
    layout: "blank",
    inNav: false,
  },
  {
    path: "/login",
    title: "Sign In",
    element: <LoginPage />,
    layout: "auth",
    inNav: false,
  },
  {
    path: "/register",
    title: "Sign Up",
    element: <RegisterPage />,
    layout: "auth",
    inNav: false,
  },
  {
    path: "/forgot-password",
    title: "Forgot Password",
    element: <ForgotPasswordPage />,
    layout: "auth",
    inNav: false,
  },
  {
    path: "/reset-password",
    title: "Reset Password",
    element: <ResetPasswordPage />,
    layout: "auth",
    inNav: false,
  },

  // --- Main Dashboard Navigation Section ---
  {
    path: "/dashboard",
    title: "Dashboard",
    icon: LayoutDashboard,
    element: <DashboardPage />,
    layout: "dashboard",
    inNav: true,
    navSection: "main",
  },
  {
    path: "/messages",
    title: "Messages",
    icon: Mail,
    element: <ListPage />,
    layout: "dashboard",
    inNav: true,
    navSection: "main",
  },
  {
    path: "/messages/:id",
    title: "Message",
    element: <DetailPage />,
    layout: "dashboard",
    inNav: false,
  },
  {
    path: "/projects",
    title: "Projects",
    icon: FolderKanban,
    element: <ProjectsPage />,
    layout: "dashboard",
    inNav: true,
    navSection: "main",
  },
  {
    path: "/reports",
    title: "Reports & Analytics",
    icon: BarChart3,
    element: <ReportsPage />,
    layout: "dashboard",
    inNav: true,
    navSection: "main",
  },

  // --- Management & Settings Section ---
  {
    path: "/profile",
    title: "Profile",
    icon: User,
    element: <ProfilePage />,
    layout: "dashboard",
    inNav: true,
    navSection: "management",
  },
  {
    path: "/settings",
    title: "Settings",
    icon: Settings,
    element: <SettingsPage />,
    layout: "dashboard",
    inNav: true,
    navSection: "management",
  },

  // --- System & Developer Section ---
  {
    path: "/example",
    title: "How to Add a Page",
    icon: HelpCircle,
    element: <ExamplePage />,
    layout: "dashboard",
    inNav: true,
    navSection: "system",
  },

  // --- Error Pages ---
  {
    path: "/403",
    title: "Access Forbidden",
    element: <ForbiddenPage />,
    layout: "blank",
    inNav: false,
  },
  {
    path: "/500",
    title: "Server Error",
    element: <ServerErrorPage />,
    layout: "blank",
    inNav: false,
  },
  {
    path: "*",
    title: "Not Found",
    element: <NotFoundPage />,
    layout: "blank",
    inNav: false,
  },
];

/**
 * Returns grouped navigation items for sidebar rendering
 */
export function getNavSections(): {
  id: "main" | "management" | "system";
  title: string;
  items: RouteConfigItem[];
}[] {
  const navItems = routesConfig.filter((route) => route.inNav);

  return [
    {
      id: "main",
      title: "Overview",
      items: navItems.filter((i) => i.navSection === "main"),
    },
    {
      id: "management",
      title: "Preferences",
      items: navItems.filter((i) => i.navSection === "management"),
    },
    {
      id: "system",
      title: "Developer Help",
      items: navItems.filter((i) => i.navSection === "system"),
    },
  ];
}
