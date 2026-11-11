import {
  LayoutDashboard,
  Mail,
  FolderKanban,
  KeyRound,
  Send,
  FileText,
  Inbox,
  Webhook,
  ShieldBan,
  BarChart3,
  Settings,
  Users,
  Bell,
  Globe,
  ShieldCheck,
  LockKeyhole,
} from "lucide-react";
import Dashboard from "@/features/mail/Dashboard";
import { Messages, MessageDetail } from "@/features/mail/Messages";
import Resources from "@/features/mail/Resources";
import Analytics from "@/features/mail/Analytics";
import SettingsPage from "@/features/mail/Settings";
import Profile from "@/features/mail/Profile";
import TemplateDetail from "@/features/mail/TemplateDetail";
import ResourceDetail from "@/features/mail/ResourceDetail";
import Privacy from "@/features/mail/Privacy";
import ProjectDetail from "@/features/mail/ProjectDetail";
import type { ComponentType } from "react";
const resource = (kind: string) => () => <Resources kind={kind} />;
export type AppRoute = {
  path: string;
  label: string;
  icon: ComponentType<{ size?: number }>;
  component: ComponentType;
  navigation: boolean;
  role?: "owner";
};
export const routes: AppRoute[] = [
  {
    path: "/dashboard",
    label: "Overview",
    icon: LayoutDashboard,
    component: Dashboard,
    navigation: true,
  },
  { path: "/messages", label: "Messages", icon: Mail, component: Messages, navigation: true },
  {
    path: "/messages/:id",
    label: "Message detail",
    icon: Mail,
    component: MessageDetail,
    navigation: false,
  },
  {
    path: "/projects",
    label: "Projects",
    icon: FolderKanban,
    component: resource("projects"),
    navigation: true,
  },
  {
    path: "/projects/:name",
    label: "Project settings",
    icon: FolderKanban,
    component: ProjectDetail,
    navigation: false,
  },
  {
    path: "/keys",
    label: "API keys",
    icon: KeyRound,
    component: resource("keys"),
    navigation: true,
  },
  {
    path: "/senders",
    label: "Senders",
    icon: Send,
    component: resource("senders"),
    navigation: true,
  },
  {
    path: "/templates",
    label: "Templates",
    icon: FileText,
    component: resource("templates"),
    navigation: true,
  },
  {
    path: "/templates/:id",
    label: "Template detail",
    icon: FileText,
    component: TemplateDetail,
    navigation: false,
  },
  {
    path: "/inbound",
    label: "Inbound",
    icon: Inbox,
    component: resource("inbound"),
    navigation: true,
  },
  {
    path: "/inbound/:id",
    label: "Inbound message",
    icon: Inbox,
    component: () => <ResourceDetail kind="inbound" />,
    navigation: false,
  },
  {
    path: "/inbound-addresses",
    label: "Inbound addresses",
    icon: Inbox,
    component: resource("inbound-addresses"),
    navigation: true,
  },
  {
    path: "/webhooks",
    label: "Webhooks",
    icon: Webhook,
    component: resource("webhooks"),
    navigation: true,
  },
  {
    path: "/webhook-deliveries",
    label: "Webhook deliveries",
    icon: Webhook,
    component: resource("webhook-deliveries"),
    navigation: true,
  },
  {
    path: "/suppressions",
    label: "Suppressions",
    icon: ShieldBan,
    component: resource("suppressions"),
    navigation: true,
  },
  {
    path: "/bounces",
    label: "Bounces",
    icon: Mail,
    component: resource("bounces"),
    navigation: true,
  },
  { path: "/alerts", label: "Alerts", icon: Bell, component: resource("alerts"), navigation: true },
  {
    path: "/alert-channels",
    label: "Alert channels",
    icon: Bell,
    component: resource("alert-channels"),
    navigation: true,
  },
  {
    path: "/domains",
    label: "Domains",
    icon: Globe,
    component: resource("domains"),
    navigation: true,
  },
  { path: "/dns", label: "DNS checks", icon: Globe, component: resource("dns"), navigation: true },
  {
    path: "/sandbox",
    label: "Sandbox",
    icon: Mail,
    component: resource("sandbox"),
    navigation: true,
  },
  {
    path: "/sandbox/:id",
    label: "Sandbox capture",
    icon: Mail,
    component: () => <ResourceDetail kind="sandbox" />,
    navigation: false,
  },
  {
    path: "/analytics",
    label: "Analytics",
    icon: BarChart3,
    component: Analytics,
    navigation: true,
  },
  {
    path: "/audit",
    label: "Audit log",
    icon: ShieldCheck,
    component: resource("audit"),
    navigation: true,
  },
  {
    path: "/settings",
    label: "Settings",
    icon: Settings,
    component: SettingsPage,
    navigation: true,
  },
  { path: "/profile", label: "Profile", icon: Users, component: Profile, navigation: false },
  {
    path: "/privacy",
    label: "Recipient privacy",
    icon: LockKeyhole,
    component: Privacy,
    navigation: true,
  },
  {
    path: "/users",
    label: "Team users",
    icon: Users,
    component: resource("users"),
    navigation: true,
    role: "owner",
  },
];
