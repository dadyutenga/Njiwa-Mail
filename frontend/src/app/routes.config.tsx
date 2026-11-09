import React, { lazy } from "react";
import {
  AlertTriangle,
  BarChart3,
  BookOpen,
  FolderKanban,
  Globe,
  Inbox,
  KeyRound,
  LayoutDashboard,
  Mail,
  ScrollText,
  Settings,
  ShieldBan,
  User,
  Users,
  Webhook,
  FileText,
  AtSign,
  FlaskConical,
  HelpCircle,
} from "lucide-react";
import type { RouteConfigItem } from "@/types/navigation";
import { roleAllowed } from "@/lib/roles";

const HomePage = lazy(() => import("@/pages/mail/HomePage"));
const NoSignupPage = lazy(() => import("@/pages/mail/HomePage").then((m) => ({ default: m.NoSignupPage })));
const LoginPage = lazy(() => import("@/pages/LoginPage"));
const OverviewPage = lazy(() => import("@/pages/mail/OverviewPage"));
const MessagesPage = lazy(() => import("@/pages/mail/MessagesPage"));
const MessagePage = lazy(() => import("@/pages/mail/MessagesPage").then((m) => ({ default: m.MessagePage })));
const UnknownPage = lazy(() => import("@/pages/mail/MessagesPage").then((m) => ({ default: m.UnknownPage })));
const BouncesPage = lazy(() => import("@/pages/mail/MessagesPage").then((m) => ({ default: m.BouncesPage })));
const InboundPage = lazy(() => import("@/pages/mail/InboundPage"));
const InboundDetailPage = lazy(() => import("@/pages/mail/InboundPage").then((m) => ({ default: m.InboundDetailPage })));
const AddressesPage = lazy(() => import("@/pages/mail/InboundPage").then((m) => ({ default: m.AddressesPage })));
const ProjectsPage = lazy(() => import("@/pages/mail/AccessPage"));
const KeysPage = lazy(() => import("@/pages/mail/AccessPage").then((m) => ({ default: m.KeysPage })));
const SendersPage = lazy(() => import("@/pages/mail/AccessPage").then((m) => ({ default: m.SendersPage })));
const TemplatesPage = lazy(() => import("@/pages/mail/TemplatesPage"));
const TemplatePage = lazy(() => import("@/pages/mail/TemplatesPage").then((m) => ({ default: m.TemplatePage })));
const WebhooksPage = lazy(() => import("@/pages/mail/OpsPage"));
const SuppressionsPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.SuppressionsPage })));
const SandboxPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.SandboxPage })));
const SandboxDetailPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.SandboxDetailPage })));
const UsersPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.UsersPage })));
const AuditPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.AuditPage })));
const MailSettingsPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.MailSettingsPage })));
const DomainsPage = lazy(() => import("@/pages/mail/ProductPage").then((m) => ({ default: m.DomainsPage })));
const AnalyticsPage = lazy(() => import("@/pages/mail/ProductPage").then((m) => ({ default: m.AnalyticsPage })));
const DocsPage = lazy(() => import("@/pages/mail/ProductPage").then((m) => ({ default: m.DocsPage })));
const SecurityPage = lazy(() => import("@/pages/mail/ProductPage").then((m) => ({ default: m.SecurityPage })));
const AlertsPage = lazy(() => import("@/pages/mail/ProductPage").then((m) => ({ default: m.AlertsPage })));
const AccountPage = lazy(() => import("@/pages/mail/OpsPage").then((m) => ({ default: m.AccountPage })));
const NotFoundPage = lazy(() => import("@/pages/NotFoundPage"));
const ForbiddenPage = lazy(() => import("@/pages/ForbiddenPage"));
const ServerErrorPage = lazy(() => import("@/pages/ServerErrorPage"));

const staff = ["owner", "admin"] as const;
const owners = ["owner"] as const;

export const routesConfig: RouteConfigItem[] = [
  { path: "/", title: "Home", element: <HomePage />, layout: "blank", inNav: false },
  { path: "/login", title: "Sign In", element: <LoginPage />, layout: "auth", inNav: false },
  { path: "/register", title: "Sign Up", element: <NoSignupPage />, layout: "auth", inNav: false },
  { path: "/forgot-password", title: "Forgot Password", element: <NoSignupPage />, layout: "auth", inNav: false },
  { path: "/reset-password", title: "Reset Password", element: <NoSignupPage />, layout: "auth", inNav: false },

  { path: "/dashboard", title: "Overview", icon: LayoutDashboard, element: <OverviewPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/messages", title: "Messages", icon: Mail, element: <MessagesPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/scheduled", title: "Scheduled", icon: Mail, element: <MessagesPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/messages/:id", title: "Message", element: <MessagePage />, layout: "dashboard", inNav: false },
  { path: "/unknown", title: "Unknown", icon: HelpCircle, element: <UnknownPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/bounces", title: "Bounces", icon: AlertTriangle, element: <BouncesPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/inbound", title: "Inbound", icon: Inbox, element: <InboundPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/inbound/:id", title: "Inbound message", element: <InboundDetailPage />, layout: "dashboard", inNav: false },
  { path: "/addresses", title: "Addresses", icon: AtSign, element: <AddressesPage />, layout: "dashboard", inNav: true, navSection: "main" },

  { path: "/projects", title: "Projects", icon: FolderKanban, element: <ProjectsPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/keys", title: "API keys", icon: KeyRound, element: <KeysPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/senders", title: "Senders and DNS", icon: Mail, element: <SendersPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/domains", title: "Domains", icon: Globe, element: <DomainsPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/analytics", title: "Analytics", icon: BarChart3, element: <AnalyticsPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/alerts", title: "Alerts", icon: AlertTriangle, element: <AlertsPage />, layout: "dashboard", inNav: true, navSection: "main" },
  { path: "/templates", title: "Templates", icon: FileText, element: <TemplatesPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/templates/:id", title: "Template", element: <TemplatePage />, layout: "dashboard", inNav: false },
  { path: "/webhooks", title: "Webhooks", icon: Webhook, element: <WebhooksPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/suppressions", title: "Suppressions", icon: ShieldBan, element: <SuppressionsPage />, layout: "dashboard", inNav: true, navSection: "management" },
  { path: "/sandbox", title: "Sandbox", icon: FlaskConical, element: <SandboxPage />, layout: "dashboard", inNav: true, navSection: "management", roles: [...staff] },
  { path: "/sandbox/:id", title: "Sandbox capture", element: <SandboxDetailPage />, layout: "dashboard", inNav: false, roles: [...staff] },

  { path: "/users", title: "Users", icon: Users, element: <UsersPage />, layout: "dashboard", inNav: true, navSection: "system", roles: [...owners] },
  { path: "/audit", title: "Audit log", icon: ScrollText, element: <AuditPage />, layout: "dashboard", inNav: true, navSection: "system", roles: [...staff] },
  { path: "/settings", title: "Settings", icon: Settings, element: <MailSettingsPage />, layout: "dashboard", inNav: true, navSection: "system" },
  { path: "/profile", title: "Account", icon: User, element: <AccountPage />, layout: "dashboard", inNav: true, navSection: "system" },
  { path: "/security", title: "Security", icon: ShieldBan, element: <SecurityPage />, layout: "dashboard", inNav: true, navSection: "system" },
  { path: "/docs", title: "API docs", icon: BookOpen, element: <DocsPage />, layout: "dashboard", inNav: true, navSection: "system" },

  { path: "/403", title: "Access Forbidden", element: <ForbiddenPage />, layout: "blank", inNav: false },
  { path: "/500", title: "Server Error", element: <ServerErrorPage />, layout: "blank", inNav: false },
  { path: "*", title: "Not Found", element: <NotFoundPage />, layout: "blank", inNav: false },
];

export function getNavSections(role?: string): {
  id: "main" | "management" | "system";
  title: string;
  items: RouteConfigItem[];
}[] {
  const navItems = routesConfig.filter((route) => route.inNav && roleAllowed(role, route.roles));
  return [
    { id: "main", title: "Mail", items: navItems.filter((item) => item.navSection === "main") },
    { id: "management", title: "Configuration", items: navItems.filter((item) => item.navSection === "management") },
    { id: "system", title: "Access", items: navItems.filter((item) => item.navSection === "system") },
  ].filter((section) => section.items.length > 0);
}
