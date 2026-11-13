import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { CheckCircle2, Circle, CircleDashed, MinusCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { useWorkspace } from "@/features/workspace/context";
import { Section } from "@/features/workspace/ui";

export type SetupStep = {
  n: number;
  id: string;
  title: string;
  status: "ready" | "needs_action" | "not_started" | "skipped_optional";
  detail: string;
  command?: string;
  route?: string;
  at?: string;
};
export type SetupJourney = {
  project: string;
  mode: string;
  next: number;
  done: number;
  total: number;
  steps: SetupStep[];
};

export type Issue = { code: string; detail: string; fix: string };
export type Candidate = {
  kind: "mailcow" | "local_mta";
  label: string;
  host: string;
  port: number;
  tls_mode: string;
  note: string;
};
export type DiscoveryReport = {
  scanned_at: string;
  host: { os: string; public_ips: string[] | null; mem_total_mb: number; cpus: number };
  ports: { port: number; address: string; process?: string; container?: string }[] | null;
  mail_services: { name: string; running: boolean; ours: boolean; in_use: boolean }[] | null;
  mailcow?: { hostname: string; running: boolean } | null;
  reverse_proxies?: string[] | null;
  recommendation: {
    mode: "managed_mta" | "existing_smtp";
    reasons: string[];
    managed_blockers: Issue[];
    warnings: Issue[];
    candidates: Candidate[];
  };
};

// Step titles are fixed here so they are translated; the server's details
// pass through phrase() and fall back to English.
const stepTitles: Record<string, string> = {
  install: "Install Njiwa-Mail",
  detect: "Detect what is already on the server",
  choose_mode: "Choose how mail is sent",
  connect: "Connect the mail server",
  domain: "Add and verify a sending domain",
  dns: "Publish the DNS records",
  sender: "Create a sender address",
  api_key: "Create an API key",
  test_send: "Send a test email",
  dashboard: "Open the dashboard",
};

export function useDiscovery() {
  return useQuery({
    queryKey: ["installation", "discovery"],
    queryFn: () =>
      admin.get<{ report: DiscoveryReport | null; recorded_at: string | null }>(
        "/installation/discovery",
      ),
  });
}

function StepMark({ status }: { status: SetupStep["status"] }) {
  if (status === "ready")
    return <CheckCircle2 size={18} className="text-success" aria-hidden="true" />;
  if (status === "skipped_optional")
    return <MinusCircle size={18} className="text-muted-foreground" aria-hidden="true" />;
  if (status === "needs_action")
    return <Circle size={18} className="text-primary" aria-hidden="true" />;
  return <CircleDashed size={18} className="text-muted-foreground" aria-hidden="true" />;
}

const statusText: Record<SetupStep["status"], string> = {
  ready: "Done",
  needs_action: "To do",
  not_started: "Not started",
  skipped_optional: "Skipped",
};

/** The ten setup steps, resumable: everything comes from server state. */
export function SetupJourneyPanel() {
  const phrase = usePhrase();
  const { to, project } = useWorkspace();
  // Steps 5-9 describe the project being viewed, so the links below open
  // the same project the steps talk about.
  const q = useQuery({
    queryKey: ["installation", "setup", project],
    queryFn: () =>
      admin.get<SetupJourney>(`/installation/setup?project=${encodeURIComponent(project)}`),
    refetchInterval: 30_000,
  });
  const j = q.data;
  if (!j?.steps) return null;
  return (
    <Section title={phrase("Set up Njiwa-Mail")}>
      <p className="mb-3 text-sm text-muted-foreground" role="status">
        {j.next === 0
          ? phrase("Setup is complete.")
          : `${phrase("Step")} ${j.next} ${phrase("of")} ${j.total} · ${j.done} ${phrase("done")}. ${phrase("You can stop at any point and continue later, from any device.")}`}
      </p>
      {j.project && (
        <p className="mb-2 text-xs text-muted-foreground">
          {phrase("Steps 5 to 9 are for project")} <span className="font-medium">{j.project}</span>
        </p>
      )}
      <ol className="space-y-2">
        {j.steps.map((st) => {
          const current = st.n === j.next;
          return (
            <li
              key={st.id}
              aria-current={current ? "step" : undefined}
              className={
                "flex gap-3 rounded-lg border p-3 text-sm " +
                (current ? "border-primary bg-primary/5" : "")
              }
            >
              <StepMark status={st.status} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">
                    {st.n}. {phrase(stepTitles[st.id] ?? st.title)}
                  </span>
                  <span className="rounded bg-muted px-1.5 text-xs">
                    {phrase(statusText[st.status])}
                  </span>
                </div>
                <p className="mt-1 text-muted-foreground">{phrase(st.detail)}</p>
                {st.status !== "ready" && st.command && (
                  <pre className="mt-2 overflow-x-auto rounded bg-muted p-2 text-xs">
                    <code>{st.command}</code>
                  </pre>
                )}
                {st.status !== "ready" && st.route && st.status !== "not_started" && (
                  <Link className="mt-1 inline-block text-xs text-primary" to={to(st.route)}>
                    {phrase("Go to this step")}
                  </Link>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    </Section>
  );
}

/** What the read-only scan found, the recommended mode and why. */
export function DiscoveryPanel({ onUseCandidate }: { onUseCandidate?: (c: Candidate) => void }) {
  const phrase = usePhrase();
  const q = useDiscovery();
  const rep = q.data?.report;
  if (q.isLoading) return null;
  if (!rep?.recommendation)
    return (
      <Section title={phrase("What is on this server")}>
        <p className="text-sm text-muted-foreground">
          {phrase(
            "Run the read-only scan on the server so Njiwa-Mail can recommend a delivery mode. It changes nothing.",
          )}
        </p>
        <pre className="mt-2 overflow-x-auto rounded bg-muted p-2 text-xs">
          <code>njiwa-mail discover -record</code>
        </pre>
      </Section>
    );
  const rec = rep.recommendation;
  const mailPorts = (rep.ports ?? []).filter((p) => [25, 465, 587, 993, 995].includes(p.port));
  return (
    <Section title={phrase("What is on this server")}>
      <p
        className="mb-3 rounded-lg border border-primary/40 bg-primary/10 p-3 text-sm"
        role="status"
      >
        <span className="font-semibold">{phrase("Recommended")}: </span>
        {phrase(
          rec.mode === "managed_mta" ? "Send from my VPS" : "Connect an existing mail server",
        )}
      </p>
      <ul className="mb-3 list-disc space-y-1 pl-5 text-sm">
        {rec.reasons.map((r) => (
          <li key={r}>{phrase(r)}</li>
        ))}
      </ul>
      <dl className="mb-3 grid gap-1 text-sm sm:grid-cols-[10rem_1fr]">
        <dt className="text-muted-foreground">{phrase("Server")}</dt>
        <dd>
          {rep.host.os || "—"} · {rep.host.cpus} CPU · {rep.host.mem_total_mb} MiB
        </dd>
        <dt className="text-muted-foreground">{phrase("Mail services")}</dt>
        <dd>
          {[
            ...(rep.mailcow ? [`Mailcow (${rep.mailcow.hostname || "?"})`] : []),
            ...(rep.mail_services ?? [])
              .filter((s) => !s.ours)
              .map((s) => s.name + (s.in_use ? ` (${phrase("in use")})` : "")),
          ].join(", ") || phrase("none found")}
        </dd>
        <dt className="text-muted-foreground">{phrase("Mail ports in use")}</dt>
        <dd>
          {mailPorts.map((p) => `${p.port} (${p.process || "?"})`).join(", ") || phrase("none")}
        </dd>
        <dt className="text-muted-foreground">{phrase("Scanned")}</dt>
        <dd>{new Date(rep.scanned_at).toLocaleString()}</dd>
      </dl>
      {rec.managed_blockers.length > 0 && (
        <div className="mb-3">
          <p className="text-sm font-semibold">
            {phrase("Why Njiwa-Mail will not install its own mail server here")}
          </p>
          <p className="mb-1 text-xs text-muted-foreground">
            {phrase("Nothing on this server is changed or replaced.")}
          </p>
          <ul className="space-y-2 text-sm">
            {rec.managed_blockers.map((b) => (
              <li key={b.code + b.detail} className="rounded border p-2">
                <code className="text-xs">{b.code}</code> {phrase(b.detail)}
                <span className="block text-xs text-muted-foreground">{phrase(b.fix)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {rec.warnings.length > 0 && (
        <div className="mb-3">
          <p className="text-sm font-semibold">{phrase("Before you continue")}</p>
          <ul className="space-y-2 text-sm">
            {rec.warnings.map((b) => (
              <li
                key={b.code + b.detail}
                className="rounded border border-warning/40 bg-warning/10 p-2"
              >
                <code className="text-xs">{b.code}</code> {phrase(b.detail)}
                <span className="block text-xs">{phrase(b.fix)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {rec.candidates.length > 0 && (
        <div>
          <p className="text-sm font-semibold">
            {phrase("Mail servers Njiwa-Mail can connect to")}
          </p>
          <ul className="space-y-2 text-sm">
            {rec.candidates.map((c) => (
              <li key={c.label} className="rounded border p-2">
                <span className="font-medium">{phrase(c.label)}</span>: {c.host}:{c.port} (
                {c.tls_mode})
                <span className="block text-xs text-muted-foreground">{phrase(c.note)}</span>
                {onUseCandidate && (
                  <Button
                    size="sm"
                    variant="outline"
                    className="mt-2"
                    onClick={() => onUseCandidate(c)}
                  >
                    {phrase("Use these settings")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </Section>
  );
}
