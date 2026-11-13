import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { AlertTriangle, CheckCircle2, Clock, Loader2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import {
  ConfirmButton,
  CopyButton,
  errorMessage,
  Field,
  inputClass,
  run,
  Section,
  selectClass,
  StateBadge,
} from "../ui";

type Existing = { id?: string; content: string; ttl?: number };
type PlanItem = {
  key: string;
  action: "create" | "unchanged" | "modify" | "conflict" | "manual";
  type: string;
  name: string;
  content: string;
  priority?: number;
  existing?: Existing;
  others?: Existing[];
  risk: "low" | "medium" | "high";
  ack?: string;
  reason: string;
  placeholder?: boolean;
};
type Warning = { code: string; severity: "info" | "warning" | "blocker"; message: string };
type Plan = {
  items: PlanItem[];
  warnings: Warning[];
  existing_mail: string[];
  blocked: boolean;
  required_acks: string[];
  rollback: string;
};
type Change = {
  id: string;
  key: string;
  action: string;
  type: string;
  name: string;
  status: string;
  note: string;
  adopted: boolean;
  new?: { content: string };
  previous?: { content: string };
};
type Setup = {
  id: string;
  status: string;
  zone_name: string;
  connection_id: string;
  connection_status: string;
  plan: Plan;
  digest: string;
  approved_at?: string | null;
  approved_by?: string;
  changes: Change[];
};
type Verification = {
  state: "verified" | "pending" | "failed" | "needs_action";
  ready: boolean;
  message: string;
  signing_proven: boolean;
  items: {
    key: string;
    type: string;
    name: string;
    state: string;
    detail: string;
    found?: string;
  }[];
};
type Connection = {
  id: string;
  auth_method: string;
  label: string;
  status: string;
  status_detail: string;
  zone_count: number;
  expires_at?: string | null;
};
type Capabilities = {
  cloudflare: { token: boolean; oauth: boolean; domain_connect: boolean };
  token_help: string;
};
type Zone = { id: string; name: string; status: string };

const STEPS = [
  "Add domain",
  "Connect Cloudflare",
  "Select zone",
  "Analyze",
  "Review",
  "Approve",
  "Apply",
  "Verify",
  "Ready",
] as const;

function stepFor(conn: Connection | undefined, setup: Setup | null, ready: boolean): number {
  if (!conn) return 1;
  if (!setup) return 2;
  switch (setup.status) {
    case "analyzed":
    case "blocked":
    case "needs_review":
      return 5;
    case "approved":
    case "applying":
    case "partial":
    case "failed":
      return 6;
    case "applied":
      return ready ? 8 : 7;
    default:
      return 2; // rolled back: start again from the zone
  }
}

function Progress({ current }: { current: number }) {
  const phrase = usePhrase();
  return (
    <ol
      aria-label={phrase("DNS setup progress")}
      className="mb-4 flex flex-wrap gap-x-1 gap-y-2 text-xs"
    >
      {STEPS.map((label, i) => {
        const done = i < current;
        const now = i === current;
        return (
          <li
            key={label}
            aria-current={now ? "step" : undefined}
            className={
              "flex items-center gap-1 rounded-full border px-2 py-1 " +
              (now
                ? "border-primary bg-primary/10 font-semibold text-primary"
                : done
                  ? "border-success/40 text-success"
                  : "text-muted-foreground")
            }
          >
            {done ? (
              <CheckCircle2 size={12} aria-hidden="true" />
            ) : (
              <span aria-hidden="true">{i + 1}</span>
            )}
            <span>{phrase(label)}</span>
            {done && <span className="sr-only">{phrase("done")}</span>}
          </li>
        );
      })}
    </ol>
  );
}

const stateTone: Record<string, string> = {
  verified: "bg-success/15 text-success",
  pending: "bg-warning/20 text-warning",
  failed: "bg-destructive/15 text-destructive",
  needs_action: "bg-warning/20 text-warning",
};

function VerifyBadge({ state }: { state: string }) {
  const phrase = usePhrase();
  const label: Record<string, string> = {
    verified: phrase("Verified"),
    pending: phrase("Pending"),
    failed: phrase("Failed"),
    needs_action: phrase("Needs action"),
  };
  return (
    <span
      className={
        "inline-flex whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium " +
        (stateTone[state] ?? "bg-muted")
      }
    >
      {label[state] ?? state}
    </span>
  );
}

// ---------------------------------------------------------------- connect

function Connections({ conns, onChanged }: { conns: Connection[]; onChanged: () => void }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const [token, setToken] = useState("");
  const [rotate, setRotate] = useState<string | null>(null);
  if (conns.length === 0) return null;
  return (
    <ul className="space-y-2">
      {conns.map((c) => (
        <li key={c.id} className="rounded-lg border p-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{c.label || phrase("Cloudflare")}</span>
            <StateBadge state={c.status} />
            <span className="text-xs text-muted-foreground">
              {c.auth_method === "oauth" ? phrase("signed in") : phrase("API token")} ·{" "}
              {c.zone_count} {phrase("zone(s) visible")}
            </span>
            <span className="ml-auto flex gap-2">
              {c.auth_method === "token" && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setRotate(rotate === c.id ? null : c.id)}
                >
                  {phrase("Replace token")}
                </Button>
              )}
              <ConfirmButton
                title={phrase("Disconnect Cloudflare?")}
                description={phrase(
                  "The saved credential is deleted. DNS records that were already added stay exactly as they are; they are not removed.",
                )}
                confirm="Disconnect"
                destructive={false}
                onConfirm={async () => {
                  await run(
                    () => api.send(`/dns/connections/${c.id}`, "DELETE"),
                    phrase("Disconnected."),
                  );
                  onChanged();
                }}
              >
                {phrase("Disconnect")}
              </ConfirmButton>
            </span>
          </div>
          {c.status !== "active" && (
            <p role="alert" className="mt-2 text-xs text-warning">
              {phrase("This connection cannot be used any more. Connect again below.")}
            </p>
          )}
          {rotate === c.id && (
            <form
              className="mt-3 flex flex-wrap items-end gap-2"
              onSubmit={async (ev) => {
                ev.preventDefault();
                const ok = await run(
                  () => api.send(`/dns/connections/${c.id}/token`, "PUT", { token }),
                  phrase("Token replaced."),
                );
                if (ok) {
                  setToken("");
                  setRotate(null);
                  onChanged();
                }
              }}
            >
              <Field label={phrase("New API token")} className="min-w-64 flex-1">
                <input
                  className={inputClass}
                  type="password"
                  autoComplete="off"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                />
              </Field>
              <Button type="submit" disabled={!token.trim()}>
                {phrase("Save token")}
              </Button>
            </form>
          )}
        </li>
      ))}
    </ul>
  );
}

function ConnectPanel({
  domainId,
  caps,
  conns,
  onChanged,
}: {
  domainId: string;
  caps?: Capabilities;
  conns: Connection[];
  onChanged: () => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const [token, setToken] = useState("");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);

  const connect = async () => {
    setBusy(true);
    const ok = await run(
      () => api.send("/dns/connections", "POST", { provider: "cloudflare", token, label }),
      phrase("Cloudflare connected."),
    );
    setBusy(false);
    if (ok) {
      setToken("");
      setLabel("");
      onChanged();
    }
  };
  const oauth = async () => {
    setBusy(true);
    try {
      const r = await api.send<{ url: string }>("/dns/oauth/start", "POST", {
        domain_id: domainId,
      });
      // Only ever follow an https authorization URL.
      if (r && r.url.startsWith("https://")) window.location.assign(r.url);
    } catch (e) {
      run(() => Promise.reject(e), "");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        {phrase(
          "Connect Cloudflare so Njiwa-Mail can add the mail records for you. You will review every change before anything is written. Nothing is changed in DNS until you approve it.",
        )}
      </p>
      <Connections conns={conns} onChanged={onChanged} />
      {caps?.cloudflare?.oauth && (
        <div>
          <Button onClick={oauth} disabled={busy}>
            {phrase("Sign in with Cloudflare")}
          </Button>
          <p className="mt-1 text-xs text-muted-foreground">
            {phrase(
              "You approve access on Cloudflare's own page. Njiwa-Mail never sees your password.",
            )}
          </p>
        </div>
      )}
      <form
        className="grid gap-3 rounded-lg border p-3"
        onSubmit={(ev) => {
          ev.preventDefault();
          void connect();
        }}
      >
        <h3 className="text-sm font-semibold">{phrase("Use an API token")}</h3>
        <p className="text-xs text-muted-foreground">
          {phrase(
            "Create a token in Cloudflare with Zone → DNS → Edit and Zone → Zone → Read, limited to the one zone you will use. Never paste a Global API Key or your password.",
          )}
        </p>
        <Field label={phrase("API token")}>
          <input
            className={inputClass}
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </Field>
        <Field label={phrase("Name (optional)")}>
          <input
            className={inputClass}
            value={label}
            maxLength={80}
            onChange={(e) => setLabel(e.target.value)}
          />
        </Field>
        <div>
          <Button type="submit" disabled={busy || !token.trim()}>
            {phrase("Connect with token")}
          </Button>
        </div>
      </form>
    </div>
  );
}

// ---------------------------------------------------------------- zone

function ZonePanel({
  domain,
  conn,
  busy,
  onStart,
}: {
  domain: string;
  conn: Connection;
  busy: boolean;
  onStart: (connId: string, zoneId: string) => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const zones = useQuery({
    queryKey: key("dns-zones", conn.id),
    queryFn: () => api.get<{ items: Zone[] }>(`/dns/connections/${conn.id}/zones`),
  });
  const items = zones.data?.items ?? [];
  const guess = items
    .filter((z) => domain === z.name || domain.endsWith("." + z.name))
    .sort((a, b) => b.name.length - a.name.length)[0]?.id;
  const [zone, setZone] = useState("");
  const chosen = zone || guess || "";
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        {phrase(
          "Choose the Cloudflare zone that contains this domain. Njiwa-Mail reads it first and changes nothing yet.",
        )}
      </p>
      {zones.isLoading && <Loader2 className="animate-spin" aria-label={phrase("Loading")} />}
      {zones.error && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(zones.error)}
        </p>
      )}
      {zones.isSuccess && items.length === 0 && (
        <p role="status" className="text-sm">
          {phrase("This credential cannot see any zone. Check the token's zone permissions.")}
        </p>
      )}
      {items.length > 0 && (
        <>
          <Field label={phrase("DNS zone")}>
            <select
              className={selectClass}
              value={chosen}
              onChange={(e) => setZone(e.target.value)}
            >
              <option value="">{phrase("Select a zone")}</option>
              {items.map((z) => (
                <option key={z.id} value={z.id}>
                  {z.name}
                </option>
              ))}
            </select>
          </Field>
          <Button disabled={!chosen || busy} onClick={() => onStart(conn.id, chosen)}>
            {busy ? phrase("Analyzing…") : phrase("Analyze this zone")}
          </Button>
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- review

const ackText: Record<string, string> = {
  create_mx: "I understand that adding an MX record changes where mail for that name is delivered.",
  modify_spf:
    "I understand that my existing SPF record will be edited in place to add the sending server, and that nothing else in it changes.",
};

function ItemRow({ it }: { it: PlanItem }) {
  const phrase = usePhrase();
  const manualCopy = it.action === "conflict" || it.action === "manual";
  return (
    <li className="rounded-lg border p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="rounded bg-muted px-1.5 text-xs">{it.type}</span>
        <code className="break-all">{it.name}</code>
        {it.risk === "high" && (
          <span className="rounded-full bg-warning/20 px-2 py-0.5 text-xs text-warning">
            {phrase("affects existing setup")}
          </span>
        )}
      </div>
      {it.action === "modify" && it.existing && (
        <dl className="mt-2 grid gap-1 text-xs sm:grid-cols-[5rem_1fr]">
          <dt className="text-muted-foreground">{phrase("Now")}</dt>
          <dd>
            <code className="break-all">{it.existing.content}</code>
          </dd>
          <dt className="text-muted-foreground">{phrase("After")}</dt>
          <dd>
            <code className="break-all">{it.content}</code>
          </dd>
        </dl>
      )}
      {(it.action === "create" || manualCopy) && !it.placeholder && (
        <div className="mt-2 flex items-start gap-2">
          <code className="break-all text-xs">
            {it.priority ? `${it.priority} ` : ""}
            {it.content}
          </code>
          <CopyButton text={it.content} label="value" />
        </div>
      )}
      {it.placeholder && (
        <code className="mt-2 block break-all text-xs italic text-muted-foreground">
          {it.content}
        </code>
      )}
      {it.others && it.others.length > 0 && (
        <p className="mt-2 text-xs text-muted-foreground">
          {phrase("Already there")}:{" "}
          <code className="break-all">{it.others.map((o) => o.content).join(" | ")}</code>
        </p>
      )}
      <p className="mt-2 text-xs text-muted-foreground">{it.reason}</p>
    </li>
  );
}

function Group({ title, items, hint }: { title: string; items: PlanItem[]; hint?: string }) {
  if (items.length === 0) return null;
  return (
    <div>
      <h3 className="mb-1 text-sm font-semibold">
        {title} ({items.length})
      </h3>
      {hint && <p className="mb-2 text-xs text-muted-foreground">{hint}</p>}
      <ul className="space-y-2">
        {items.map((it) => (
          <ItemRow key={it.key + it.type + it.name} it={it} />
        ))}
      </ul>
    </div>
  );
}

function ReviewPanel({
  setup,
  busy,
  onApprove,
  onAnalyze,
}: {
  setup: Setup;
  busy: boolean;
  onApprove: (acks: string[]) => void;
  onAnalyze: () => void;
}) {
  const phrase = usePhrase();
  const plan = setup.plan;
  const by = (a: PlanItem["action"]) => plan.items.filter((i) => i.action === a);
  const [acked, setAcked] = useState<Record<string, boolean>>({});
  const actionable = by("create").length + by("modify").length;
  const allAcked = plan.required_acks.every((a) => acked[a]);
  const approvable = setup.status === "analyzed" && !plan.blocked && actionable > 0;
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        {phrase("Zone")}: <strong>{setup.zone_name}</strong>.{" "}
        {phrase("Nothing has been written. Review the exact changes below.")}
      </p>
      {setup.status === "needs_review" && (
        <p role="alert" className="rounded-lg border border-warning/40 bg-warning/15 p-3 text-sm">
          {phrase(
            "DNS changed after you approved, so nothing was written. Review the updated plan and approve again.",
          )}
        </p>
      )}
      {plan.warnings.length > 0 && (
        <ul className="space-y-2" aria-label={phrase("Warnings")}>
          {plan.warnings.map((w) => (
            <li
              key={w.code}
              role={w.severity === "blocker" ? "alert" : undefined}
              className={
                "flex gap-2 rounded-lg border p-3 text-sm " +
                (w.severity === "blocker"
                  ? "border-destructive/40 bg-destructive/10"
                  : w.severity === "warning"
                    ? "border-warning/40 bg-warning/15"
                    : "bg-muted/40")
              }
            >
              {w.severity === "blocker" ? (
                <XCircle
                  size={16}
                  className="mt-0.5 shrink-0 text-destructive"
                  aria-hidden="true"
                />
              ) : (
                <AlertTriangle size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
              )}
              <span>
                <span className="sr-only">{w.severity}: </span>
                {w.message}
              </span>
            </li>
          ))}
        </ul>
      )}
      {plan.existing_mail.length > 0 && (
        <p className="text-sm">
          {phrase("Existing email detected")}: <strong>{plan.existing_mail.join(", ")}</strong>
        </p>
      )}
      <Group title={phrase("Will be created")} items={by("create")} />
      <Group
        title={phrase("Will be changed")}
        items={by("modify")}
        hint={phrase(
          "Your existing record is edited in place. It keeps its TTL and everything else in it.",
        )}
      />
      <Group title={phrase("Already in place, left unchanged")} items={by("unchanged")} />
      <Group
        title={phrase("Needs you: conflicts")}
        items={by("conflict")}
        hint={phrase(
          "Njiwa-Mail will not change these. Resolve them in Cloudflare, then analyze again.",
        )}
      />
      <Group
        title={phrase("Cannot be automated")}
        items={by("manual")}
        hint={phrase("Njiwa-Mail does not have a safe value for these. Publish them yourself.")}
      />
      <div className="rounded-lg border bg-muted/30 p-3 text-xs">
        <strong>{phrase("If something goes wrong")}: </strong>
        {plan.rollback}
      </div>
      {approvable && plan.required_acks.length > 0 && (
        <fieldset className="space-y-2 rounded-lg border p-3">
          <legend className="px-1 text-sm font-semibold">{phrase("Confirm these changes")}</legend>
          {plan.required_acks.map((a) => (
            <label key={a} className="flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-1"
                checked={!!acked[a]}
                onChange={(e) => setAcked({ ...acked, [a]: e.target.checked })}
              />
              <span>{ackText[a] ? phrase(ackText[a]) : a}</span>
            </label>
          ))}
        </fieldset>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          disabled={!approvable || !allAcked || busy}
          onClick={() => onApprove(plan.required_acks)}
        >
          {phrase("Approve these exact changes")}
        </Button>
        <Button variant="outline" disabled={busy} onClick={onAnalyze}>
          {phrase("Analyze again")}
        </Button>
      </div>
      {!approvable && (
        <p className="text-xs text-muted-foreground">
          {plan.blocked
            ? phrase("Fix the blocking problem above, then analyze again.")
            : actionable === 0
              ? phrase("There is nothing to write. Resolve the items above or verify the domain.")
              : ""}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- apply / changes

function Changes({ changes }: { changes: Change[] }) {
  const phrase = usePhrase();
  if (changes.length === 0) return null;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-xs">
        <caption className="sr-only">{phrase("Changes made to DNS")}</caption>
        <thead>
          <tr className="text-muted-foreground">
            <th className="py-1 pr-3">{phrase("Record")}</th>
            <th className="py-1 pr-3">{phrase("Action")}</th>
            <th className="py-1 pr-3">{phrase("Result")}</th>
            <th className="py-1">{phrase("Note")}</th>
          </tr>
        </thead>
        <tbody>
          {changes.map((c) => (
            <tr key={c.id} className="border-t align-top">
              <td className="py-1 pr-3">
                <span className="rounded bg-muted px-1">{c.type}</span>{" "}
                <code className="break-all">{c.name}</code>
              </td>
              <td className="py-1 pr-3">
                {c.action === "update" ? phrase("edited") : phrase("created")}
              </td>
              <td className="py-1 pr-3">
                <StateBadge
                  state={
                    c.status.includes("applied")
                      ? "active"
                      : c.status === "failed"
                        ? "revoked"
                        : "draft"
                  }
                  label={c.status.replace(/_/g, " ")}
                />
              </td>
              <td className="py-1">
                {c.adopted ? phrase("already present; left as it was") : c.note}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ---------------------------------------------------------------- verify

function VerifyPanel({
  setup,
  result,
  checking,
  attempt,
  onCheck,
}: {
  setup: Setup;
  result: Verification | null;
  checking: boolean;
  attempt: number;
  onCheck: () => void;
}) {
  const phrase = usePhrase();
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        {result ? <VerifyBadge state={result.state} /> : <Clock size={16} aria-hidden="true" />}
        <span className="text-sm" role="status">
          {result?.message ?? phrase("Checking DNS…")}
        </span>
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          disabled={checking}
          onClick={onCheck}
        >
          {checking ? phrase("Checking…") : phrase("Check now")}
        </Button>
      </div>
      {result?.state === "pending" && (
        <p className="text-xs text-muted-foreground">
          {phrase(
            "DNS can take a while to propagate. Njiwa-Mail checks again automatically, and keeps checking in the background for 48 hours.",
          )}{" "}
          ({attempt})
        </p>
      )}
      {result && (
        <ul className="space-y-2">
          {result.items.map((i) => (
            <li key={i.key + i.name} className="rounded-lg border p-3 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <VerifyBadge state={i.state} />
                <span className="rounded bg-muted px-1.5 text-xs">{i.type}</span>
                <code className="break-all text-xs">{i.name}</code>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">{i.detail}</p>
            </li>
          ))}
        </ul>
      )}
      {result?.ready && !result.signing_proven && (
        <p className="rounded-lg border bg-muted/30 p-3 text-xs">
          {phrase(
            "Publishing a DKIM record does not prove your mail is signed. Send a test message to your own mailbox and use Proof that mail is signed on this page before sending real mail.",
          )}
        </p>
      )}
      <Changes changes={setup.changes} />
    </div>
  );
}

// ---------------------------------------------------------------- the wizard

const POLL_LIMIT = 25;
const pollDelay = (n: number) => (n < 4 ? 15_000 : n < 10 ? 60_000 : 180_000);

export function DnsSetup({
  domainId,
  domain,
  onChanged,
}: {
  domainId: string;
  domain: string;
  onChanged: () => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [busy, setBusy] = useState(false);
  const [ver, setVer] = useState<Verification | null>(null);
  const [checking, setChecking] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [manual, setManual] = useState(false);

  const caps = useQuery({
    queryKey: key("dns-capabilities"),
    queryFn: () => api.get<Capabilities>("/dns/capabilities"),
  });
  const conns = useQuery({
    queryKey: key("dns-connections"),
    queryFn: () => api.get<{ items: Connection[] }>("/dns/connections"),
  });
  const latest = useQuery({
    queryKey: key("dns-setup", domainId),
    queryFn: () => api.get<{ setup: Setup | null }>(`/domains/${domainId}/dns-setup`),
  });
  const setup = latest.data?.setup ?? null;
  const active = (conns.data?.items ?? []).filter((c) => c.status === "active");
  const conn = active.find((c) => c.id === setup?.connection_id) ?? active[0];
  const done = setup && ["rolled_back", "rollback_partial"].includes(setup.status) ? null : setup;
  const current = stepFor(conn, done, !!ver?.ready);

  const refresh = useCallback(async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: key("dns-setup", domainId) }),
      client.invalidateQueries({ queryKey: key("dns-connections") }),
    ]);
  }, [client, key, domainId]);

  // Returning from Cloudflare sign-in: say what happened, then clean the URL.
  useEffect(() => {
    if (params.get("dns_connection")) {
      run(() => Promise.resolve(true), phrase("Cloudflare connected."));
    } else if (params.get("dns_error")) {
      run(
        () =>
          Promise.reject(
            new Error(
              phrase("Cloudflare sign-in did not complete. Try again or use an API token."),
            ),
          ),
        "",
      );
    } else return;
    const next = new URLSearchParams(params);
    next.delete("dns_connection");
    next.delete("dns_error");
    setParams(next, { replace: true });
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const act = async <T,>(fn: () => Promise<T>, ok: string) => {
    setBusy(true);
    const r = await run(fn, ok);
    setBusy(false);
    await refresh();
    return r;
  };

  const verify = useCallback(async () => {
    if (!setup) return;
    setChecking(true);
    try {
      const r = await api.send<Verification>(`/dns-setups/${setup.id}/verify`, "POST");
      setVer(r);
      onChanged();
      return r;
    } catch (e) {
      run(() => Promise.reject(e), "");
    } finally {
      setChecking(false);
    }
  }, [api, setup, onChanged]);

  // Verification polls in a bounded way: a growing delay, at most POLL_LIMIT checks.
  const verifyRef = useRef(verify);
  verifyRef.current = verify;
  const status = setup?.status;
  const state = ver?.state;
  useEffect(() => {
    if (status !== "applied") return;
    if (state === "verified" || attempt >= POLL_LIMIT) return;
    const t = setTimeout(
      () => {
        void verifyRef.current().then(() => setAttempt((n) => n + 1));
      },
      attempt === 0 && !ver ? 500 : pollDelay(attempt),
    );
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, state, attempt, setup?.id]);

  const start = (connId: string, zoneId: string) =>
    act(
      () =>
        api.send<Setup>(`/domains/${domainId}/dns-setup`, "POST", {
          connection_id: connId,
          zone_id: zoneId,
        }),
      phrase("Analysis finished."),
    );

  const body = (() => {
    if (conns.isLoading || latest.isLoading)
      return <Loader2 className="animate-spin" aria-label={phrase("Loading")} />;
    if (!conn || (conns.data && active.length === 0)) {
      return (
        <ConnectPanel
          domainId={domainId}
          caps={caps.data}
          conns={conns.data?.items ?? []}
          onChanged={refresh}
        />
      );
    }
    if (!done) return <ZonePanel domain={domain} conn={conn} busy={busy} onStart={start} />;
    switch (done.status) {
      case "analyzed":
      case "blocked":
      case "needs_review":
        return (
          <ReviewPanel
            setup={done}
            busy={busy}
            onApprove={(acks) =>
              act(
                () =>
                  api.send(`/dns-setups/${done.id}/approve`, "POST", {
                    digest: done.digest,
                    acknowledge: acks,
                  }),
                phrase("Approved."),
              )
            }
            onAnalyze={() =>
              act(
                () => api.send(`/dns-setups/${done.id}/analyze`, "POST"),
                phrase("Analysis finished."),
              )
            }
          />
        );
      case "approved":
      case "applying":
        return (
          <div className="space-y-3">
            <p className="text-sm">
              {phrase("Approved by")} <strong>{done.approved_by}</strong>.{" "}
              {phrase(
                "The approval expires after 30 minutes. Applying writes the records you reviewed, and nothing else.",
              )}
            </p>
            <ConfirmButton
              title={phrase("Apply the approved DNS changes?")}
              description={phrase(
                "The records you approved will be written to Cloudflare. If DNS changed since you reviewed it, nothing is written.",
              )}
              confirm="Apply changes"
              destructive={false}
              variant="default"
              size="default"
              disabled={busy || done.status === "applying"}
              onConfirm={async () => {
                await act(
                  () => api.send(`/dns-setups/${done.id}/apply`, "POST"),
                  phrase("Records written. Now checking DNS."),
                );
                setAttempt(0);
                setVer(null);
              }}
            >
              {done.status === "applying" ? phrase("Applying…") : phrase("Apply to Cloudflare")}
            </ConfirmButton>
          </div>
        );
      case "partial":
      case "failed":
        return (
          <div className="space-y-3">
            <p
              role="alert"
              className="rounded-lg border border-warning/40 bg-warning/15 p-3 text-sm"
            >
              {done.status === "partial"
                ? phrase("Only some of the records were written. The setup is not complete.")
                : phrase("No record was written.")}{" "}
              {phrase(
                "Nothing is marked as done. Analyze again to see what remains, or roll back what was added.",
              )}
            </p>
            <Changes changes={done.changes} />
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={busy}
                onClick={() =>
                  act(
                    () => api.send(`/dns-setups/${done.id}/analyze`, "POST"),
                    phrase("Analysis finished."),
                  )
                }
              >
                {phrase("Analyze again")}
              </Button>
              {done.changes.length > 0 && <RollbackButton id={done.id} busy={busy} act={act} />}
            </div>
          </div>
        );
      case "applied":
        return (
          <div className="space-y-3">
            <VerifyPanel
              setup={done}
              result={ver}
              checking={checking}
              attempt={attempt}
              onCheck={() => void verify()}
            />
            <RollbackButton id={done.id} busy={busy} act={act} />
          </div>
        );
      default:
        return null;
    }
  })();

  return (
    <Section
      className="mt-4"
      title={phrase("Set up DNS automatically with Cloudflare")}
      action={
        <Button size="sm" variant="ghost" onClick={() => setManual(!manual)} aria-expanded={manual}>
          {manual ? phrase("Hide manual setup") : phrase("My DNS is somewhere else")}
        </Button>
      }
    >
      <Progress current={current} />
      {conn && !manual && (
        <div className="mb-4">
          <Connections
            conns={(conns.data?.items ?? []).filter((c) => c.status === "active")}
            onChanged={refresh}
          />
        </div>
      )}
      {manual ? <ManualSetup domainId={domainId} /> : body}
    </Section>
  );
}

function RollbackButton({
  id,
  busy,
  act,
}: {
  id: string;
  busy: boolean;
  act: <T>(fn: () => Promise<T>, ok: string) => Promise<T | undefined>;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  return (
    <ConfirmButton
      title={phrase("Roll back the records Njiwa-Mail added?")}
      description={phrase(
        "Only records Njiwa-Mail created are removed, and an SPF record it edited is put back, but only while nobody has changed them since. Anything changed by someone else is left alone and listed. Mail that relies on these records may stop working.",
      )}
      confirm="Roll back"
      disabled={busy}
      onConfirm={async () => {
        await act(
          () => api.send(`/dns-setups/${id}/rollback`, "POST"),
          phrase("Rollback finished."),
        );
      }}
    >
      {phrase("Roll back")}
    </ConfirmButton>
  );
}

function ManualSetup({ domainId }: { domainId: string }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const q = useQuery({
    queryKey: key("dns-manual", domainId),
    queryFn: () =>
      api.get<{
        note: string;
        records: {
          key: string;
          type: string;
          host: string;
          short_host: string;
          value: string;
          priority?: number;
          placeholder?: boolean;
          hint?: string;
        }[];
      }>(`/domains/${domainId}/dns-manual`),
  });
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        {phrase(
          "Publish these records at your DNS provider, then use Check now on this page. Mail hostnames must be DNS only, never proxied.",
        )}
      </p>
      {q.isLoading && <Loader2 className="animate-spin" aria-label={phrase("Loading")} />}
      {q.error && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(q.error)}
        </p>
      )}
      <ul className="space-y-2">
        {q.data?.records.map((r) => (
          <li key={r.key + r.host} className="rounded-lg border p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <span className="rounded bg-muted px-1.5 text-xs">{r.type}</span>
              <code className="break-all">{r.short_host}</code>
              <CopyButton text={r.short_host} label="host" />
            </div>
            <div className="mt-2 flex items-start gap-2">
              <code
                className={
                  "break-all text-xs " + (r.placeholder ? "italic text-muted-foreground" : "")
                }
              >
                {r.priority ? `${r.priority} ` : ""}
                {r.value}
              </code>
              {!r.placeholder && <CopyButton text={r.value} label="value" />}
            </div>
            {r.placeholder && (
              <p className="mt-1 text-xs text-warning">
                {phrase(
                  "Template, not a record to copy: replace the part in <…> with your mail server's details before publishing.",
                )}
              </p>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
