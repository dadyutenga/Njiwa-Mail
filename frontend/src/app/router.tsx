import { Suspense, type ReactNode } from "react";
import { createBrowserRouter, Link, Navigate } from "react-router-dom";
import Welcome from "@/features/welcome/Welcome";
import { ProtectedRoute } from "@/features/auth/auth";
import AuthPage from "@/pages/Auth";
import Status from "@/pages/Status";
import { PageLoader } from "@/components/shared/Display";
import { ErrorBoundary, NotFound } from "@/components/shared/RouteErrors";
import { LegacyWorkspaceRoute, WorkspaceRoute } from "@/features/workspace/Layout";
import { AppResolver, Checklist, Dashboard } from "@/features/workspace/pages/Overview";
import {
  CaptureDetail,
  Inbox,
  MessageDetail,
  MessageLog,
  Sandbox,
  Scheduled,
  Sent,
  TempInboxes,
  UnknownQueue,
} from "@/features/workspace/pages/Mail";
import { DnsGuide, DomainDetail, Domains } from "@/features/workspace/pages/Domains";
import { Complaints, Mailboxes, Senders, Suppressions } from "@/features/workspace/pages/Workspace";
import { Templates } from "@/features/workspace/pages/Templates";
import { Automations } from "@/features/workspace/pages/Automations";
import { Bulk, CampaignDetail, NewCampaign } from "@/features/workspace/pages/Bulk";
import { ContactDetail, Contacts, Lists, Segments } from "@/features/workspace/pages/Audience";
import { ApiDocs, ApiKeys, WebhookDetail, Webhooks } from "@/features/workspace/pages/Developers";
import {
  Alerts,
  Analytics,
  Audit,
  MailServer,
  Privacy,
  Projects,
  System,
  Users,
} from "@/features/workspace/pages/Admin";
import ProjectDetail from "@/features/mail/ProjectDetail";
import SettingsPage from "@/features/mail/Settings";
import Profile from "@/features/mail/Profile";
import { Empty } from "@/features/workspace/ui";
import { DomainClaims } from "@/features/installation/Claims";
import { MtaQueue } from "@/features/installation/MtaQueue";
import { MailHost } from "@/features/installation/MailHost";

const page = (el: ReactNode) => <Suspense fallback={<PageLoader />}>{el}</Suspense>;

function WorkspaceNotFound() {
  return (
    <Empty
      title="Page not found"
      description="This page does not exist in the workspace."
      action={
        <Link className="text-primary underline" to="../dashboard" relative="path">
          Go to the Dashboard
        </Link>
      }
    />
  );
}

const workspacePages: [string, ReactNode][] = [
  ["dashboard", <Dashboard />],
  ["setup", <Checklist />],
  ["inbox", <Inbox />],
  ["sent", <Sent />],
  ["scheduled", <Scheduled />],
  ["contacts", <Contacts />],
  ["contacts/:id", <ContactDetail />],
  ["lists", <Lists />],
  ["segments", <Segments />],
  ["bulk", <Bulk />],
  ["bulk/new", <NewCampaign />],
  ["bulk/:id", <CampaignDetail />],
  ["temp-inboxes", <TempInboxes />],
  ["captures", <Sandbox />],
  ["captures/:id", <CaptureDetail />],
  ["domains", <Domains />],
  ["domains/:id", <DomainDetail />],
  ["dns-guide", <DnsGuide />],
  ["mailboxes", <Mailboxes />],
  ["senders", <Senders />],
  ["templates", <Templates />],
  ["automations", <Automations />],
  ["suppressions", <Suppressions />],
  ["complaints", <Complaints />],
  ["keys", <ApiKeys />],
  ["webhooks", <Webhooks />],
  ["webhooks/:id", <WebhookDetail />],
  ["messages", <MessageLog />],
  ["messages/:id", <MessageDetail />],
  ["unknown", <UnknownQueue />],
  ["api-docs", <ApiDocs />],
  ["projects", <Projects />],
  ["projects/:name", <ProjectDetail />],
  ["users", <Users />],
  ["mail-server", <MailServer />],
  ["domain-claims", <DomainClaims />],
  ["mta-queue", <MtaQueue />],
  ["mail-host", <MailHost />],
  ["analytics", <Analytics />],
  ["alerts", <Alerts />],
  ["system", <System />],
  ["audit", <Audit />],
  ["settings", <SettingsPage />],
  ["profile", <Profile />],
  ["privacy", <Privacy />],
];

// Old flat routes from before the workspace: send them to the resolver.
const legacy = [
  "dashboard",
  "messages",
  "messages/:id",
  "projects",
  "projects/:name",
  "keys",
  "senders",
  "templates",
  "templates/:id",
  "inbound",
  "inbound/:id",
  "inbound-addresses",
  "webhooks",
  "webhook-deliveries",
  "suppressions",
  "bounces",
  "alerts",
  "alert-channels",
  "domains",
  "dns",
  "sandbox",
  "sandbox/:id",
  "analytics",
  "audit",
  "users",
];

export const router = createBrowserRouter([
  { path: "/", element: <Welcome /> },
  {
    path: "/app",
    errorElement: <ErrorBoundary />,
    element: (
      <ProtectedRoute>
        <AppResolver />
      </ProtectedRoute>
    ),
  },
  {
    path: "/p/:project",
    errorElement: <ErrorBoundary />,
    element: (
      <ProtectedRoute>
        <WorkspaceRoute />
      </ProtectedRoute>
    ),
    children: [
      { index: true, element: <Navigate to="dashboard" replace /> },
      ...workspacePages.map(([path, el]) => ({ path, element: page(el) })),
      { path: "*", element: <WorkspaceNotFound /> },
    ],
  },
  ...(["live", "sandbox"] as const).map((mode) => ({
    path: `/p/:project/${mode}/*`,
    element: (
      <ProtectedRoute>
        <LegacyWorkspaceRoute mode={mode} />
      </ProtectedRoute>
    ),
  })),
  { path: "/settings", element: <Navigate to="/app?next=settings" replace /> },
  { path: "/profile", element: <Navigate to="/app?next=profile" replace /> },
  { path: "/privacy", element: <Navigate to="/app?next=privacy" replace /> },
  ...legacy.map((path) => ({ path: "/" + path, element: <Navigate to="/app" replace /> })),
  ...["login", "forgot-password", "reset-password"].map((path) => ({
    path,
    element: <AuthPage mode={path} />,
  })),
  { path: "403", element: <Status code="403" title="Access Denied" /> },
  { path: "404", element: <Status code="404" title="Page Not Found" /> },
  { path: "500", element: <Status code="500" title="Server Error" /> },
  { path: "*", element: <NotFound /> },
]);
