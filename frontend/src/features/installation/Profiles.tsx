import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { useWorkspace } from "@/features/workspace/context";
import {
  ConfirmButton,
  DataState,
  Field,
  inputClass,
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  Time,
} from "@/features/workspace/ui";
import { installationKey, statusLabels, useInstallation, type StepStatus } from "./Installation";

// Delivery profiles and the planned mode switch (backlog B08). Every number
// and blocker comes from the backend switch planner; a switch with pending
// mail needs an explicit acknowledgement, and unknown outcomes always block.

type Profile = {
  id: string;
  mode: "managed_mta" | "existing_smtp";
  label: string;
  provider_type: string;
  config_revision: number;
  status: string;
  is_active: boolean;
  last_tested_at?: string;
  config: { host?: string; port?: number; submission_host?: string };
};

type Plan = {
  target_id: string;
  target_mode: string;
  current_id?: string;
  already_active: boolean;
  queued: number;
  scheduled: number;
  submitting: number;
  retry_wait: number;
  unknown: number;
  verified: boolean;
  blockers: string[];
  needs_decision: boolean;
};

const modeLabel: Record<string, string> = {
  managed_mta: "Send from my VPS",
  existing_smtp: "Existing mail server",
};

const blockerText: Record<string, string> = {
  profile_not_verified:
    "Run the connection test for this profile first; it must pass for its current settings.",
  profile_incomplete: "This profile has no host or password yet.",
  managed_not_installed:
    "Run the host installer on the server first (njiwa-mail installer apply -record).",
  unknown_jobs_require_reconciliation:
    "Some messages have an unknown outcome. Record their real outcome on the Unknown page first; they are never moved to another server.",
};

const profilesKey = ["transport-profiles"] as const;

function SwitchReview({ p, onDone }: { p: Profile; onDone: () => void }) {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const [ack, setAck] = useState(false);
  const plan = useQuery({
    queryKey: ["transport-plan", p.id],
    queryFn: () => admin.get<Plan>(`/installation/transport-profiles/${p.id}/activation-plan`),
  });
  return (
    <DataState q={plan} rows={2}>
      {(pl) => {
        const pending = pl.queued + pl.scheduled + pl.submitting + pl.retry_wait;
        const blocked = pl.blockers.length > 0;
        return (
          <div className="mt-3 rounded-lg border p-3 text-sm" data-testid="switch-review">
            <p className="font-medium">
              {phrase("Switch to")} {p.label} ({phrase(modeLabel[p.mode])})
            </p>
            <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-5">
              {(
                [
                  ["Queued", pl.queued],
                  ["Scheduled", pl.scheduled],
                  ["Sending", pl.submitting],
                  ["Waiting to retry", pl.retry_wait],
                  ["Unknown outcome", pl.unknown],
                ] as [string, number][]
              ).map(([k, v]) => (
                <div key={k}>
                  <dt className="text-xs text-muted-foreground">{phrase(k)}</dt>
                  <dd className="font-semibold">{v}</dd>
                </div>
              ))}
            </dl>
            {blocked && (
              <ul className="mt-3 space-y-1" role="alert">
                {pl.blockers.map((b) => (
                  <li key={b} className="text-destructive">
                    {phrase(blockerText[b] ?? b)}
                    {b === "unknown_jobs_require_reconciliation" && (
                      <>
                        {" "}
                        <Link className="text-primary underline" to={to("unknown")}>
                          {phrase("Open Unknown")}
                        </Link>
                      </>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {!blocked && pl.needs_decision && (
              <label className="mt-3 flex items-start gap-2">
                <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
                <span>
                  {phrase("Send the")} {pending}{" "}
                  {phrase(
                    "pending Live messages through the new profile. Their sender domains may need new SPF/DKIM records for it.",
                  )}
                </span>
              </label>
            )}
            {!blocked && (
              <div className="mt-3">
                <ConfirmButton
                  title={phrase("Switch mail delivery?")}
                  description={phrase(
                    "Live mail will go out through this profile from now on. The current profile is kept, and you can switch back the same way.",
                  )}
                  confirm={phrase("Switch")}
                  destructive={false}
                  variant="default"
                  disabled={pl.needs_decision && !ack}
                  onConfirm={async () => {
                    const r = await run(
                      () =>
                        admin.send(`/installation/transport-profiles/${p.id}/activate`, "POST", {
                          migrate_pending: pl.needs_decision && ack,
                        }),
                      phrase("Mail delivery switched."),
                    );
                    if (r) onDone();
                  }}
                >
                  {phrase("Switch to this profile")}
                </ConfirmButton>
              </div>
            )}
          </div>
        );
      }}
    </DataState>
  );
}

function NewSMTPProfile({ onDone }: { onDone: () => void }) {
  const phrase = usePhrase();
  const [f, setF] = useState({
    label: "",
    host: "",
    port: "587",
    tls_mode: "starttls",
    username: "",
    password: "",
  });
  const set = (k: keyof typeof f, v: string) => setF({ ...f, [k]: v });
  return (
    <form
      className="mt-3 grid gap-3 rounded-lg border p-3 sm:grid-cols-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const r = await run(
          () =>
            admin.send("/installation/transport-profiles", "POST", {
              mode: "existing_smtp",
              label: f.label,
              smtp: {
                host: f.host,
                port: Number(f.port),
                tls_mode: f.tls_mode,
                username: f.username,
                password: f.password,
              },
            }),
          phrase("Profile added. Run its connection test before switching to it."),
        );
        if (r) onDone();
      }}
    >
      <Field label={phrase("Label")}>
        <input
          className={inputClass}
          value={f.label}
          onChange={(e) => set("label", e.target.value)}
        />
      </Field>
      <Field label={phrase("SMTP host")}>
        <input
          className={inputClass}
          value={f.host}
          onChange={(e) => set("host", e.target.value.trim())}
        />
      </Field>
      <Field label={phrase("Port")}>
        <input
          className={inputClass}
          inputMode="numeric"
          value={f.port}
          onChange={(e) => set("port", e.target.value)}
        />
      </Field>
      <Field label={phrase("Encryption")}>
        <select
          className={selectClass}
          value={f.tls_mode}
          onChange={(e) => set("tls_mode", e.target.value)}
        >
          <option value="starttls">STARTTLS (587)</option>
          <option value="implicit">TLS (465)</option>
        </select>
      </Field>
      <Field label={phrase("Username")}>
        <input
          className={inputClass}
          autoComplete="off"
          value={f.username}
          onChange={(e) => set("username", e.target.value)}
        />
      </Field>
      <Field label={phrase("Password")}>
        <input
          className={inputClass}
          type="password"
          autoComplete="new-password"
          value={f.password}
          onChange={(e) => set("password", e.target.value)}
        />
      </Field>
      <div className="sm:col-span-2">
        <Button type="submit" disabled={!f.label || !f.host}>
          {phrase("Add profile")}
        </Button>
      </div>
    </form>
  );
}

/** The profile list with test, review-switch, add and retire actions. */
export function TransportProfiles() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const inst = useInstallation();
  const q = useQuery({
    queryKey: profilesKey,
    queryFn: () => admin.get<{ profiles: Profile[] }>("/installation/transport-profiles"),
  });
  const [review, setReview] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const refresh = () => {
    client.invalidateQueries({ queryKey: profilesKey });
    client.invalidateQueries({ queryKey: installationKey });
    client.invalidateQueries({ queryKey: ["transport-plan"] });
    client.invalidateQueries({ queryKey: ["ws"] });
  };
  const previous = s(
    (inst.data as { previous_profile_id?: string } | undefined)?.previous_profile_id,
  );
  return (
    <Section
      title={phrase("Mail delivery profiles")}
      action={
        <Button size="sm" variant="outline" onClick={() => setAdding(!adding)}>
          {adding ? phrase("Cancel") : phrase("Add an SMTP profile")}
        </Button>
      }
    >
      <p className="mb-3 text-xs text-muted-foreground">
        {phrase(
          "Exactly one profile sends Live mail. Switching is planned: pending mail is reviewed first, and messages with an unknown outcome are never moved.",
        )}
      </p>
      {adding && (
        <NewSMTPProfile
          onDone={() => {
            setAdding(false);
            refresh();
          }}
        />
      )}
      <DataState q={q} rows={2}>
        {(d) => (
          <ul className="space-y-3">
            {(d.profiles ?? []).map((p) => (
              <li key={p.id} className="rounded-lg border p-3" data-profile={p.id}>
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-semibold">{p.label}</span>
                  <span className="rounded bg-muted px-1.5 text-xs">
                    {phrase(modeLabel[p.mode])}
                  </span>
                  {p.is_active && <StateBadge state="pass" label={phrase("Active")} />}
                  {!p.is_active && p.id === previous && (
                    <span className="text-xs text-muted-foreground">
                      ({phrase("previous profile, for rollback")})
                    </span>
                  )}
                  <span className="text-xs text-muted-foreground">
                    {phrase("revision")} {p.config_revision} ·{" "}
                    {phrase(statusLabels[p.status as StepStatus] ?? p.status)}
                    {p.last_tested_at && (
                      <>
                        {" "}
                        · {phrase("tested")} <Time value={p.last_tested_at} relative />
                      </>
                    )}
                  </span>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {p.mode === "existing_smtp"
                    ? `${s(p.config.host) || phrase("no host")}:${s(p.config.port)}`
                    : s(p.config.submission_host) || phrase("installer not run yet")}
                </p>
                <div className="mt-2 flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={testing === p.id}
                    onClick={async () => {
                      setTesting(p.id);
                      await run(
                        () => admin.send(`/installation/transport-profiles/${p.id}/test`, "POST"),
                        phrase("Connection test recorded."),
                      );
                      setTesting(null);
                      refresh();
                    }}
                  >
                    {testing === p.id ? phrase("Testing…") : phrase("Test connection")}
                  </Button>
                  {!p.is_active && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setReview(review === p.id ? null : p.id)}
                    >
                      {phrase(p.id === previous ? "Review switching back" : "Review switch")}
                    </Button>
                  )}
                  {!p.is_active && (
                    <ConfirmButton
                      title={phrase("Retire this profile?")}
                      description={phrase(
                        "It is hidden and its stored passwords are deleted. Messages already sent through it keep their history.",
                      )}
                      confirm={phrase("Retire")}
                      onConfirm={async () => {
                        await run(
                          () => admin.send(`/installation/transport-profiles/${p.id}`, "DELETE"),
                          phrase("Profile retired."),
                        );
                        refresh();
                      }}
                    >
                      {phrase("Retire")}
                    </ConfirmButton>
                  )}
                </div>
                {review === p.id && (
                  <SwitchReview
                    p={p}
                    onDone={() => {
                      setReview(null);
                      refresh();
                    }}
                  />
                )}
              </li>
            ))}
          </ul>
        )}
      </DataState>
    </Section>
  );
}

/** Compact delivery-infrastructure status for every project dashboard (B09). */
export function DeliveryHealthCard() {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const inst = useInstallation();
  const st = useQuery({
    queryKey: ["mail-server", "status"],
    queryFn: () => admin.get<Record<string, unknown>>("/mail-server/status"),
    refetchInterval: 30_000,
  });
  const rd = inst.data?.readiness;
  const n = (v: unknown) => (typeof v === "number" ? v : Number(v) || 0);
  return (
    <Section
      title={phrase("Delivery infrastructure")}
      action={
        <Link className="text-sm text-primary" to={to("mail-server")}>
          {phrase("Open")}
        </Link>
      }
    >
      <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-5" data-testid="delivery-health">
        <div>
          <p className="text-xs text-muted-foreground">{phrase("Mode")}</p>
          <p className="font-semibold">
            {rd?.mode ? phrase(modeLabel[rd.mode]) : phrase("Not chosen")}
          </p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{phrase("Status")}</p>
          <p className="font-semibold">{rd ? phrase(statusLabels[rd.status] ?? rd.status) : "…"}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{phrase("In the queue")}</p>
          <p className="font-semibold">{n(st.data?.queue_depth)}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{phrase("Waiting to retry")}</p>
          <p className="font-semibold">{n(st.data?.retry_wait)}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">{phrase("Unknown outcome")}</p>
          <p className="font-semibold">{n(st.data?.unknown)}</p>
        </div>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {phrase("Last handed to the mail server")}:{" "}
        <Time value={st.data?.last_submitted_at} relative />
      </p>
    </Section>
  );
}
