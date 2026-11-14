import {
  Activity,
  AlertOctagon,
  BarChart3,
  Bell,
  BookOpen,
  Bot,
  CalendarClock,
  ClipboardCheck,
  Contact,
  Filter,
  FileText,
  FolderKanban,
  Globe,
  HelpCircle,
  Inbox,
  KeyRound,
  LayoutDashboard,
  ListChecks,
  ListPlus,
  Mailbox,
  Megaphone,
  ScrollText,
  Send,
  Server,
  Settings,
  ShieldBan,
  ShieldCheck,
  TestTube2,
  Timer,
  UserRoundCheck,
  Users,
  Webhook,
} from "lucide-react";
import type { ComponentType } from "react";

export type NavItem = {
  path: string; // relative to /p/{project}/
  label: string;
  icon: ComponentType<{ size?: number; className?: string }>;
  badge?: "unread";
  /** Only installation operators see this entry. */
  operator?: boolean;
};
export type NavGroup = { label: string; items: NavItem[] };

export const navGroups: NavGroup[] = [
  {
    label: "Overview",
    items: [
      { path: "dashboard", label: "Dashboard", icon: LayoutDashboard },
      { path: "setup", label: "Getting started", icon: ListChecks },
    ],
  },
  {
    label: "Mail",
    items: [
      { path: "inbox", label: "Inbox", icon: Inbox, badge: "unread" },
      { path: "sent", label: "Sent", icon: Send },
      { path: "scheduled", label: "Scheduled", icon: CalendarClock },
      { path: "bulk", label: "Bulk send", icon: Megaphone },
      { path: "temp-inboxes", label: "Temporary inboxes", icon: Timer },
      { path: "captures", label: "Sandbox", icon: TestTube2 },
    ],
  },
  {
    label: "Audience",
    items: [
      { path: "contacts", label: "Contacts", icon: Contact },
      { path: "lists", label: "Lists", icon: ListPlus },
      { path: "segments", label: "Segments", icon: Filter },
    ],
  },
  {
    label: "Workspace",
    items: [
      { path: "domains", label: "Domains", icon: Globe },
      { path: "dns-guide", label: "DNS setup guide", icon: HelpCircle },
      { path: "mailboxes", label: "Mailboxes", icon: Mailbox },
      { path: "senders", label: "Sender addresses", icon: UserRoundCheck },
      { path: "templates", label: "Templates", icon: FileText },
      { path: "automations", label: "Automations", icon: Bot },
      { path: "suppressions", label: "Suppressions", icon: ShieldBan },
      { path: "complaints", label: "Complaints", icon: AlertOctagon },
    ],
  },
  {
    label: "Developers",
    items: [
      { path: "keys", label: "API keys", icon: KeyRound },
      { path: "webhooks", label: "Webhooks", icon: Webhook },
      { path: "messages", label: "Message log", icon: ScrollText },
      { path: "unknown", label: "Unknown queue", icon: ClipboardCheck },
      { path: "api-docs", label: "API docs", icon: BookOpen },
    ],
  },
  {
    label: "Admin",
    items: [
      { path: "projects", label: "Projects", icon: FolderKanban },
      { path: "users", label: "Users", icon: Users, operator: true },
      { path: "mail-server", label: "Delivery infrastructure", icon: Server, operator: true },
      { path: "domain-claims", label: "Domain ownership", icon: Globe, operator: true },
      { path: "mta-queue", label: "Mail queue", icon: ListChecks, operator: true },
      { path: "mail-host", label: "Mail host", icon: Server, operator: true },
      { path: "analytics", label: "Analytics", icon: BarChart3 },
      { path: "alerts", label: "Alerts", icon: Bell },
      { path: "announcements", label: "Announcements", icon: Megaphone, operator: true },
      { path: "system", label: "System", icon: Activity },
      { path: "audit", label: "Audit log", icon: ShieldCheck },
      { path: "settings", label: "Settings", icon: Settings },
    ],
  },
];

/** Labels for detail pages, used by breadcrumbs. */
export const detailLabels: Record<string, string> = {
  profile: "Profile",
  privacy: "Recipient privacy",
  new: "New",
};

export function findNav(segment: string): { group: NavGroup; item: NavItem } | undefined {
  for (const group of navGroups) {
    const item = group.items.find((i) => i.path === segment);
    if (item) return { group, item };
  }
  return undefined;
}
