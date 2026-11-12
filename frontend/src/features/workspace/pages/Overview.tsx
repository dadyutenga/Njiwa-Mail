import { useState } from "react";
import { Link, Navigate, useLocation, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, Circle, FolderKanban, Plus } from "lucide-react";
import { usePhrase } from "@/features/i18n/i18n";
import { statusLabels, type StepStatus } from "@/features/installation/Installation";
import { DeliveryHealthCard } from "@/features/installation/Profiles";
import { Button } from "@/components/ui/button";
import { lastWorkspace, useProjects, useWorkspace, useWsKey } from "../context";
import { FirstRunShell, NewProjectDialog, useCompose } from "../Layout";
import { DataState, ErrorState, LoadingRows, PageHeader, Section, StateBadge } from "../ui";

// ---------------------------------------------------------------- /app: choose the workspace

/**
 * Resolves where a signed-in user lands: the last used workspace (if it still
 * exists), the only project, or the first-run screen. ?next=profile|settings
 * keeps legacy links working.
 */
export function AppResolver() {
  const phrase = usePhrase();
  const projects = useProjects();
  const [params] = useSearchParams();
  const [creating, setCreating] = useState(false);
  const next = params.get("next") ?? "";
  const choose = params.get("choose") === "1";
  if (projects.isPending)
    return (
      <FirstRunShell>
        <LoadingRows />
      </FirstRunShell>
    );
  if (projects.error)
    return (
      <FirstRunShell>
        <ErrorState error={projects.error} retry={() => projects.refetch()} />
      </FirstRunShell>
    );
  const list = projects.data;
  if (!list.length) return <FirstRun />;
  const last = lastWorkspace();
  const target =
    last && list.some((p) => p.name === last.project)
      ? last
      : list.length === 1
        ? { project: list[0].name }
        : null;
  if (target && !choose) {
    const page = ["profile", "settings", "privacy"].includes(next) ? next : "dashboard";
    return <Navigate to={`/p/${encodeURIComponent(target.project)}/${page}`} replace />;
  }
  return (
    <FirstRunShell>
      <h1 className="text-2xl font-bold">{phrase("Choose a project")}</h1>
      <ul className="mt-6 grid gap-3 sm:grid-cols-2">
        {list.map((p) => (
          <li key={p.id}>
            <Link
              to={`/p/${encodeURIComponent(p.name)}/dashboard`}
              className="flex items-center gap-3 rounded-xl border bg-card p-4 hover:border-primary"
            >
              <FolderKanban size={20} className="text-primary" />
              <span className="font-semibold">{p.name}</span>
              {!p.enabled && <StateBadge state="cancelled" label={phrase("disabled")} />}
            </Link>
          </li>
        ))}
      </ul>
      <Button className="mt-6" variant="outline" onClick={() => setCreating(true)}>
        <Plus size={16} /> {phrase("New project")}
      </Button>
      <NewProjectDialog open={creating} onOpenChange={setCreating} />
    </FirstRunShell>
  );
}

function FirstRun() {
  const phrase = usePhrase();
  const [creating, setCreating] = useState(false);
  const steps = [
    "Connect mail server",
    "Create a project",
    "Add a domain",
    "Publish DNS records",
    "Add a sender address",
    "Create an API key",
    "Send a test email",
  ];
  return (
    <FirstRunShell>
      <div className="rounded-2xl border bg-card p-6 sm:p-10">
        <p className="text-sm font-semibold uppercase tracking-wide text-primary">
          {phrase("Welcome")}
        </p>
        <h1 className="mt-2 text-3xl font-bold">{phrase("Set up Njiwa-Mail")}</h1>
        <p className="mt-2 text-muted-foreground">
          {phrase(
            "Start with a project. Each project has a Sandbox, where mail is captured, and a Live environment for real mail.",
          )}
        </p>
        <ol className="mt-6 space-y-2 text-sm">
          {steps.map((st, i) => (
            <li key={st} className="flex items-center gap-2">
              <span className="grid h-6 w-6 place-items-center rounded-full border text-xs">
                {i + 1}
              </span>
              {phrase(st)}
            </li>
          ))}
        </ol>
        <Button className="mt-8" onClick={() => setCreating(true)}>
          <Plus size={16} /> {phrase("Create your first project")}
        </Button>
        <p className="mt-4 text-xs text-muted-foreground">
          {phrase(
            "Connecting the mail server is done from the Mail server page inside the project; you can do it first.",
          )}
        </p>
      </div>
      <NewProjectDialog open={creating} onOpenChange={setCreating} />
    </FirstRunShell>
  );
}

// ---------------------------------------------------------------- dashboard

type Overview = {
  counts: Record<string, number>;
  submitted_24h: number;
  queue_depth: number;
  scheduled: number;
  unknown: number;
  unread: number;
  hard_bounces_7d: number;
  complaints_7d: number;
  suppressions: number;
  webhook_failed: number;
  active_campaigns: number;
};

export function Dashboard() {
  const phrase = usePhrase();
  const { api, to, env } = useWorkspace();
  const key = useWsKey();
  const compose = useCompose();
  const q = useQuery({
    queryKey: key("overview"),
    queryFn: () => api.get<Overview>("/overview"),
    refetchInterval: 30_000,
  });
  const checklist = useQuery({
    queryKey: key("checklist"),
    queryFn: () => api.get<ChecklistResponse>("/checklist"),
  });
  if (checklist.data && !checklist.data.complete) return <Navigate to={to("setup")} replace />;
  return (
    <>
      <PageHeader
        title={phrase("Dashboard")}
        description={
          env === "live"
            ? phrase("Live mail in this project.")
            : phrase("Sandbox mail in this project. Nothing here reaches real recipients.")
        }
        action={<Button onClick={() => compose.open()}>{phrase("Compose")}</Button>}
      />
      {checklist.data && <InstallationBanner inst={checklist.data.installation} />}
      <div className="mb-6">
        <DeliveryHealthCard />
      </div>
      <DataState q={q} rows={4}>
        {(d) => {
          const cards: [string, number, string][] = [
            ["Submitted, last 24 hours", d.submitted_24h, "sent?state=submitted"],
            ["In the queue", d.queue_depth, "messages?state=queued"],
            ["Scheduled", d.scheduled, "scheduled"],
            ["Unread in Inbox", d.unread, "inbox?unread=1"],
            ["Unknown outcome", d.unknown, "unknown"],
            ["Hard bounces, 7 days", d.hard_bounces_7d, "suppressions?source=hard_bounce"],
            ["Complaints, 7 days", d.complaints_7d, "complaints"],
            ["Failed webhook deliveries", d.webhook_failed, "webhooks"],
          ];
          return (
            <div className="space-y-6">
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                {cards.map(([label, value, link]) => (
                  <Link
                    key={label}
                    to={to(link)}
                    className="rounded-xl border bg-card p-4 transition hover:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <p className="text-xs text-muted-foreground sm:text-sm">{phrase(label)}</p>
                    <p className="mt-2 text-2xl font-bold sm:text-3xl">{value}</p>
                  </Link>
                ))}
              </div>
              <Section title={phrase("Messages by state")}>
                <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                  {Object.entries(d.counts).map(([state, count]) => (
                    <li key={state}>
                      <Link
                        to={to(`messages?state=${state}`)}
                        className="flex items-center justify-between rounded-lg border px-3 py-2 text-sm hover:bg-muted"
                      >
                        <StateBadge state={state} />
                        <span className="font-semibold">{count}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
                <p className="mt-3 text-xs text-muted-foreground">
                  {phrase(
                    "“submitted” means the mail server accepted the message. Njiwa-Mail does not claim delivery to the inbox.",
                  )}
                </p>
              </Section>
            </div>
          );
        }}
      </DataState>
    </>
  );
}

// ---------------------------------------------------------------- setup checklist

type Step = {
  key: string;
  title: string;
  done: boolean;
  required: boolean;
  optional?: boolean;
  detail?: string;
};

// The project list and the installation summary are separate scopes: project
// progress never includes the mail transport, and the denominator always
// comes from the backend.
type InstallationSummary = {
  scope: "installation";
  status: StepStatus;
  mode?: "managed_mta" | "existing_smtp";
  progress: { complete: number; required: number };
  welcome_state: string;
  environment_override?: boolean;
};

type ChecklistResponse = {
  scope: "project";
  complete: boolean;
  steps: Step[];
  progress: { complete: number; required: number };
  installation: InstallationSummary;
};

function InstallationBanner({ inst }: { inst: InstallationSummary }) {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  if (inst.status === "ready") return null;
  const text =
    inst.status === "not_started"
      ? "Mail delivery for this installation is not set up yet. Sandbox works now; live mail waits until delivery is ready."
      : inst.status === "stale"
        ? "Mail delivery settings changed or were last checked too long ago. Live mail may not go out until they are checked again."
        : "Mail delivery for this installation needs attention. Live mail stays queued until it is ready.";
  return (
    <div
      role="status"
      className="mb-4 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm"
      data-installation-status={inst.status}
    >
      <p>
        <span className="font-medium">{phrase("Delivery infrastructure")}:</span>{" "}
        {phrase(statusLabels[inst.status] ?? inst.status)} ({inst.progress.complete}/
        {inst.progress.required}). {phrase(text)}
      </p>
      <Link className="mt-1 inline-block font-medium text-primary" to={to("mail-server")}>
        {phrase("Open Delivery infrastructure")}
      </Link>
    </div>
  );
}

const stepLinks: Record<string, { path: string; action: string; help: string }> = {
  domain: {
    path: "domains",
    action: "Add domain",
    help: "Add the domain you will send from (and receive on, if needed).",
  },
  dns: {
    path: "domains",
    action: "Open Domains",
    help: "Publish SPF, DKIM and DMARC (and MX for receiving), then check them.",
  },
  sender: {
    path: "senders",
    action: "Add a sender",
    help: "An address on a verified domain, for example hello@yourdomain.",
  },
  api_key: {
    path: "keys",
    action: "Create an API key",
    help: "Your application uses it to send. The secret is shown once.",
  },
  test_email: {
    path: "sandbox",
    action: "Send a test",
    help: "Send one message in Sandbox (it is captured), then one in Live.",
  },
  mailbox: {
    path: "mailboxes",
    action: "Create a mailbox",
    help: "Optional: an address that receives mail into the Inbox.",
  },
};

export function Checklist() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const location = useLocation();
  const compose = useCompose();
  const q = useQuery({
    queryKey: key("checklist"),
    queryFn: () => api.get<ChecklistResponse>("/checklist"),
  });
  const created = (location.state as { created?: boolean } | null)?.created;
  return (
    <>
      <PageHeader
        title={phrase("Getting started")}
        description={phrase(
          "The steps for this project. Each shows its real state; open any step directly. Sandbox works before setup is complete.",
        )}
      />
      {created && (
        <p
          role="status"
          className="mb-4 rounded-lg border border-primary/40 bg-primary/10 p-3 text-sm"
        >
          {phrase("Project created with a Sandbox and a Live environment.")}
        </p>
      )}
      <DataState q={q}>
        {(d) => {
          const { complete: doneCount, required } = d.progress;
          return (
            <>
              <InstallationBanner inst={d.installation} />
              <div className="mb-4 flex items-center gap-3">
                <div
                  className="h-2 flex-1 overflow-hidden rounded-full bg-muted"
                  role="progressbar"
                  aria-label={phrase("Project setup")}
                  aria-valuemin={0}
                  aria-valuemax={required}
                  aria-valuenow={doneCount}
                >
                  <div
                    className="h-full bg-primary transition-all"
                    style={{ width: required ? `${(doneCount / required) * 100}%` : "0%" }}
                  />
                </div>
                <span className="text-sm text-muted-foreground" data-testid="project-progress">
                  {doneCount}/{required}
                </span>
              </div>
              {d.complete && (
                <p
                  role="status"
                  className="mb-4 rounded-lg border border-success/40 bg-success/10 p-3 text-sm"
                >
                  {phrase("Setup is complete. The Dashboard is now your landing page.")}{" "}
                  <Link to={to("dashboard")} className="font-medium text-primary">
                    {phrase("Open Dashboard →")}
                  </Link>
                </p>
              )}
              <ol className="space-y-3">
                {d.steps.map((st, i) => {
                  const link = stepLinks[st.key];
                  if (!link) return null;
                  return (
                    <li
                      key={st.key}
                      className="flex flex-wrap items-start gap-3 rounded-xl border bg-card p-4"
                    >
                      {st.done ? (
                        <CheckCircle2 className="mt-0.5 text-success" aria-label="done" />
                      ) : (
                        <Circle className="mt-0.5 text-muted-foreground" aria-label="not done" />
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="font-semibold">
                          {i + 1}. {phrase(st.title)}{" "}
                          {st.optional && (
                            <span className="text-xs font-normal text-muted-foreground">
                              ({phrase("optional")})
                            </span>
                          )}
                        </p>
                        <p className="text-sm text-muted-foreground">{phrase(link.help)}</p>
                        {st.detail && (
                          <p className="mt-1 text-xs text-warning">{phrase(st.detail)}</p>
                        )}
                        {st.key === "dns" && (
                          <Link
                            className="mt-1 inline-block text-xs text-primary underline"
                            to={to("dns-guide")}
                          >
                            {phrase("DNS setup guide")}
                          </Link>
                        )}
                      </div>
                      {st.key === "test_email" ? (
                        <Button
                          size="sm"
                          variant={st.done ? "outline" : "default"}
                          onClick={() => compose.open()}
                        >
                          {phrase("Compose a test")}
                        </Button>
                      ) : (
                        <Button asChild size="sm" variant={st.done ? "outline" : "default"}>
                          <Link to={to(link.path)}>{phrase(st.done ? "Open" : link.action)}</Link>
                        </Button>
                      )}
                    </li>
                  );
                })}
              </ol>
            </>
          );
        }}
      </DataState>
    </>
  );
}
