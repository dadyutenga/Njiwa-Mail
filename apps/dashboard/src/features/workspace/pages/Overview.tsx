import { useState } from "react";
import { Link, Navigate, useLocation, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowUpRight,
  Ban,
  CalendarClock,
  CheckCircle2,
  Circle,
  FolderKanban,
  HelpCircle,
  Inbox,
  Info,
  Layers,
  MessageSquareWarning,
  Plus,
  Send,
  Webhook,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { usePhrase } from "@/features/i18n/i18n";
import { statusLabels, type StepStatus } from "@/features/installation/Installation";
import { DeliveryHealthCard } from "@/features/installation/Profiles";
import { Button } from "@/components/ui/button";
import { lastWorkspace, useProjects, useWorkspace, useWsKey } from "../context";
import { FirstRunShell, NewProjectDialog, useCompose } from "../Layout";
import { DataState, ErrorState, LoadingRows, PageHeader, Section, StateBadge, Time } from "../ui";

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
              className="od-card flex-row items-center hover:border-[var(--od-border-strong)]"
            >
              <FolderKanban size={20} className="text-[var(--od-series-1)]" />
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
      <div className="od-card">
        <p className="od-caption font-semibold uppercase tracking-wide">{phrase("Welcome")}</p>
        <h1 className="od-page-title">{phrase("Set up Njiwa-Mail")}</h1>
        <p className="od-caption">
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
        {(d) => (
          <div className="space-y-6">
            <KpiGroup
              title={phrase("Sending")}
              cards={[
                {
                  label: "Submitted, last 24 hours",
                  value: d.submitted_24h,
                  link: "sent?state=submitted",
                  icon: Send,
                  tone: "success",
                },
                {
                  label: "In the queue",
                  value: d.queue_depth,
                  link: "messages?state=queued",
                  icon: Layers,
                  tone: "neutral",
                },
                {
                  label: "Scheduled",
                  value: d.scheduled,
                  link: "scheduled",
                  icon: CalendarClock,
                  tone: "neutral",
                },
                {
                  label: "Unread in Inbox",
                  value: d.unread,
                  link: "inbox?unread=1",
                  icon: Inbox,
                  tone: "primary",
                },
              ]}
            />
            <KpiGroup
              title={phrase("Needs attention")}
              cards={[
                {
                  label: "Unknown outcome",
                  value: d.unknown,
                  link: "unknown",
                  icon: HelpCircle,
                  tone: "warning",
                },
                {
                  label: "Hard bounces, 7 days",
                  value: d.hard_bounces_7d,
                  link: "suppressions?source=hard_bounce",
                  icon: Ban,
                  tone: "danger",
                },
                {
                  label: "Complaints, 7 days",
                  value: d.complaints_7d,
                  link: "complaints",
                  icon: MessageSquareWarning,
                  tone: "danger",
                },
                {
                  label: "Failed webhook deliveries",
                  value: d.webhook_failed,
                  link: "webhooks",
                  icon: Webhook,
                  tone: "danger",
                },
              ]}
            />
            <MessagesByState counts={d.counts} />
          </div>
        )}
      </DataState>
    </>
  );
}

// ---------------------------------------------------------------- dashboard widgets

type Tone = "success" | "primary" | "warning" | "danger" | "neutral";

const toneStyles: Record<Tone, { icon: string; active: string }> = {
  success: { icon: "bg-success/15 text-success", active: "" },
  primary: { icon: "bg-chart-1/15 text-chart-1", active: "" },
  neutral: { icon: "bg-[var(--od-neutral-soft)] text-[var(--od-ink-secondary)]", active: "" },
  warning: {
    icon: "bg-[var(--od-warning-soft)] text-warning",
    active: "border-[var(--od-warning-mark)]",
  },
  danger: {
    icon: "bg-destructive/15 text-destructive",
    active: "border-[var(--od-bad-mark)]",
  },
};

type Kpi = { label: string; value: number; link: string; icon: LucideIcon; tone: Tone };

function KpiGroup({ title, cards }: { title: string; cards: Kpi[] }) {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  return (
    <section aria-label={title}>
      <h2 className="od-section-title mb-2">{title}</h2>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {cards.map(({ label, value, link, icon: Icon, tone }) => {
          const st = toneStyles[tone];
          // Problem cards stay quiet at zero and only draw the eye when they count something.
          const muted = value === 0 && (tone === "warning" || tone === "danger");
          return (
            <Link
              key={label}
              to={to(link)}
              className={cn(
                "od-card justify-between transition hover:border-[var(--od-border-strong)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                !muted && st.active,
              )}
            >
              <div className="flex items-start justify-between gap-2">
                <p className="od-kpi-label">{phrase(label)}</p>
                <span
                  className={cn(
                    "grid h-8 w-8 shrink-0 place-items-center rounded-[var(--od-radius-sm)]",
                    muted ? "bg-[var(--od-neutral-soft)] text-[var(--od-ink-muted)]" : st.icon,
                  )}
                >
                  <Icon size={16} aria-hidden />
                </span>
              </div>
              <div className="flex items-end justify-between">
                <p className="od-kpi-value">{value.toLocaleString()}</p>
                <ArrowUpRight
                  size={16}
                  aria-hidden
                  className="text-muted-foreground opacity-0 transition group-hover:opacity-100"
                />
              </div>
            </Link>
          );
        })}
      </div>
    </section>
  );
}

// Message states grouped by what they mean for the sender. Colours match StateBadge.
type StateGroup = {
  title: string;
  tone: "good" | "progress" | "bad" | "muted";
  states: { key: string; label: string; color: string }[];
};
const stateGroups: StateGroup[] = [
  {
    title: "Accepted",
    tone: "good",
    states: [
      { key: "submitted", label: "Submitted", color: "var(--od-good-mark)" },
      { key: "captured", label: "Captured", color: "var(--od-series-5)" },
    ],
  },
  {
    title: "In progress",
    tone: "progress",
    states: [
      { key: "queued", label: "Queued", color: "var(--od-muted-mark)" },
      { key: "submitting", label: "Submitting", color: "var(--od-series-8)" },
      { key: "scheduled", label: "Scheduled", color: "var(--od-series-3)" },
      { key: "retry_wait", label: "Retrying", color: "var(--od-warning-mark)" },
    ],
  },
  {
    title: "Problems",
    tone: "bad",
    states: [
      { key: "failed", label: "Failed", color: "var(--od-bad-mark)" },
      { key: "expired", label: "Expired", color: "var(--od-series-4)" },
      { key: "unknown", label: "Unknown", color: "var(--od-series-6)" },
    ],
  },
  {
    title: "Stopped",
    tone: "muted",
    states: [{ key: "cancelled", label: "Cancelled", color: "var(--od-muted-mark)" }],
  },
];

const groupInk: Record<StateGroup["tone"], string> = {
  good: "od-delta-good",
  progress: "od-delta-neutral",
  bad: "od-delta-bad",
  muted: "od-delta-neutral",
};

function MessagesByState({ counts }: { counts: Record<string, number> }) {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const known = new Set(stateGroups.flatMap((g) => g.states.map((s) => s.key)));
  // Any state the backend adds later still shows up instead of disappearing.
  const extra = Object.keys(counts).filter((k) => !known.has(k));
  const groups: StateGroup[] = [
    ...stateGroups,
    ...(extra.length
      ? [
          {
            title: "Other",
            tone: "muted" as const,
            states: extra.map((key) => ({
              key,
              label: key.replace(/_/g, " "),
              color: "var(--od-series-7)",
            })),
          },
        ]
      : []),
  ];
  const all = groups.flatMap((g) => g.states);
  const n = (key: string) => counts[key] ?? 0;
  const total = all.reduce((sum, st) => sum + n(st.key), 0);
  const pct = (v: number) => (total ? Math.round((v / total) * 100) : 0);
  const sumOf = (g: StateGroup) => g.states.reduce((sum, st) => sum + n(st.key), 0);

  return (
    <article className="od-card" aria-labelledby="messages-by-state-title">
      <div className="flex flex-col gap-5">
        <header className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 id="messages-by-state-title" className="od-card-title">
              {phrase("Messages by state")}
            </h2>
            <p className="od-card-subtitle">
              {phrase("Where every message in this project is now.")}
            </p>
          </div>
          <Link
            to={to("messages")}
            className="inline-flex items-center gap-1 rounded-[var(--od-radius-sm)] px-2 py-1 text-sm font-medium text-[var(--od-ink-secondary)] transition hover:bg-[var(--od-neutral-soft)] hover:text-[var(--od-ink)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {phrase("All messages")}
            <ArrowUpRight size={14} aria-hidden />
          </Link>
        </header>

        {/* Headline: the total, then one figure per group. */}
        <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3 lg:grid-cols-5">
          <div>
            <dt className="od-kpi-label">{phrase("Total")}</dt>
            <dd className="od-kpi-value">{total.toLocaleString()}</dd>
          </div>
          {groups.map((g) => {
            const sum = sumOf(g);
            return (
              <div key={g.title} className="border-l border-[var(--od-border)] pl-4">
                <dt className="od-kpi-label">{phrase(g.title)}</dt>
                <dd className="flex items-baseline gap-2">
                  <span className="od-kpi-value">{sum.toLocaleString()}</span>
                  {sum > 0 && <span className={cn("od-delta", groupInk[g.tone])}>{pct(sum)}%</span>}
                </dd>
              </div>
            );
          })}
        </dl>

        {/* Share of every state; segments are separated so small ones stay visible. */}
        <div
          className="flex h-2.5 w-full gap-[3px] overflow-hidden rounded-full bg-[var(--od-neutral-soft)]"
          role="img"
          aria-label={phrase("Share of messages in each state")}
        >
          {all.map(({ key, label, color }) =>
            n(key) ? (
              <div
                key={key}
                className="h-full min-w-1.5 rounded-full"
                style={{ width: `${(n(key) / total) * 100}%`, background: color }}
                title={`${phrase(label)}: ${n(key)} (${pct(n(key))}%)`}
              />
            ) : null,
          )}
        </div>

        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {groups.map((g) => {
            const active = g.states.filter((st) => n(st.key) > 0);
            const idle = g.states.filter((st) => n(st.key) === 0);
            return (
              <section
                key={g.title}
                aria-label={phrase(g.title)}
                className={cn(
                  "flex flex-col gap-2 rounded-[var(--od-radius-md)] border border-[var(--od-border)] bg-[var(--od-surface-subtle)] p-3",
                  g.tone === "bad" && active.length > 0 && "border-[var(--od-bad-mark)]/40",
                )}
              >
                <h3 className="od-kpi-label">{phrase(g.title)}</h3>
                {active.length > 0 ? (
                  <ul className="flex flex-col gap-1">
                    {active.map(({ key, label, color }) => (
                      <li key={key}>
                        <Link
                          to={to(`messages?state=${key}`)}
                          className="-mx-1.5 block rounded-[var(--od-radius-sm)] px-1.5 py-1 transition hover:bg-[var(--od-neutral-soft)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          <span className="flex items-center gap-2 text-sm">
                            <span
                              className="size-2 shrink-0 rounded-full"
                              style={{ background: color }}
                              aria-hidden
                            />
                            <span className="flex-1 font-medium text-[var(--od-ink)]">
                              {phrase(label)}
                            </span>
                            <span className="text-xs tabular-nums text-[var(--od-ink-muted)]">
                              {pct(n(key))}%
                            </span>
                            <span className="min-w-8 text-right font-semibold tabular-nums text-[var(--od-ink)]">
                              {n(key).toLocaleString()}
                            </span>
                          </span>
                          <span
                            className="mt-1.5 block h-1 overflow-hidden rounded-full bg-[var(--od-neutral-soft)]"
                            aria-hidden
                          >
                            <span
                              className="block h-full rounded-full"
                              style={{ width: `${pct(n(key))}%`, background: color }}
                            />
                          </span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="od-caption">{phrase("Nothing here right now.")}</p>
                )}
                {idle.length > 0 && (
                  <div className="mt-auto flex flex-wrap gap-1 pt-1">
                    {idle.map(({ key, label }) => (
                      <Link
                        key={key}
                        to={to(`messages?state=${key}`)}
                        className="rounded-full border border-[var(--od-border)] px-2 py-0.5 text-xs text-[var(--od-ink-muted)] transition hover:border-[var(--od-border-strong)] hover:text-[var(--od-ink-secondary)]"
                      >
                        {phrase(label)} · 0
                      </Link>
                    ))}
                  </div>
                )}
              </section>
            );
          })}
        </div>

        <p className="od-caption flex items-start gap-2">
          <Info size={14} className="mt-0.5 shrink-0" aria-hidden />
          {phrase(
            "“submitted” means the mail server accepted the message. Njiwa-Mail does not claim delivery to the inbox.",
          )}
        </p>
      </div>
    </article>
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
  live_handover_at?: string | null;
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
  // Never say mail may not go out when it demonstrably just did. A failing
  // delivery is reported as an error, which keeps the warning.
  const handedOver = inst.status !== "error" ? inst.live_handover_at : null;
  const text =
    inst.status === "not_started"
      ? "Mail delivery for this installation is not set up yet. Sandbox works now; live mail waits until delivery is ready."
      : handedOver
        ? "Some delivery checks need attention. Your mail server recently accepted live mail."
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
        {handedOver && (
          <>
            {" "}
            {phrase("Last handed over")}: <Time value={handedOver} />
          </>
        )}
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
    help: "Send one message in Live. Once the mail server accepts it, this step is complete. Sandbox testing is optional.",
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
                    <li key={st.key} className="od-card flex-row flex-wrap items-start">
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
