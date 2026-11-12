import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Circle, Clock, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { DataState, run, Section, Time } from "@/features/workspace/ui";

// Installation-level delivery setup (backlog B01-B03). Everything shown here
// comes from GET /admin/v1/installation: the client never guesses readiness
// and never hard-codes a step count.

export type DeliveryMode = "managed_mta" | "existing_smtp";

export type StepStatus =
  | "not_started"
  | "checking"
  | "ready"
  | "needs_action"
  | "blocked_external_action"
  | "error"
  | "skipped_optional"
  | "stale";

export type ReadinessStep = {
  id: string;
  title: string;
  status: StepStatus;
  required: boolean;
  checked_at?: string;
  problem_code?: string;
  message?: string;
};

export type Readiness = {
  scope: "installation";
  mode?: DeliveryMode;
  status: StepStatus;
  progress: { complete: number; required: number };
  steps: ReadinessStep[];
};

export type InstallationState = {
  readiness: Readiness;
  welcome_state:
    | "ready_to_configure"
    | "legacy_detected"
    | "in_progress"
    | "degraded"
    | "completed";
  wizard: { selected_mode?: DeliveryMode; confirmed_at?: string };
  active_profile: {
    id: string;
    mode: DeliveryMode;
    config_revision: number;
    sending_facts?: SendingFacts;
  } | null;
  profile: { id: string; mode: DeliveryMode; config_revision: number } | null;
  environment_override?: boolean;
};

export type SendingFacts = {
  egress_ips?: string[];
  spf_include?: string;
  dkim_selector?: string;
  dkim_keys?: Record<string, string>;
};

export const installationKey = ["installation"] as const;

/**
 * Where an existing SMTP server sends from (C07). Njiwa-Mail cannot see a
 * server it does not run; with these facts SPF and DKIM are checked against
 * the real values instead of templates.
 */
export function SendingFactsForm({
  profileId,
  facts,
}: {
  profileId: string;
  facts?: SendingFacts;
}) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [ips, setIps] = useState((facts?.egress_ips ?? []).join(", "));
  const [include, setInclude] = useState(facts?.spf_include ?? "");
  const [selector, setSelector] = useState(facts?.dkim_selector ?? "");
  const [saving, setSaving] = useState(false);
  return (
    <Section title={phrase("What your mail server sends from")}>
      <p className="mb-3 text-xs text-muted-foreground">
        {phrase(
          "Njiwa-Mail cannot see the server it hands mail to. Enter what your mail server's operator documents, so SPF and DKIM are checked against the real values. These do not change the connection settings.",
        )}
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="grid gap-1.5 text-sm">
          <span className="font-medium">{phrase("Sending IP addresses or ranges")}</span>
          <input
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
            value={ips}
            onChange={(e) => setIps(e.target.value)}
            placeholder="198.51.100.7, 2001:db8:5::/48"
          />
        </label>
        <label className="grid gap-1.5 text-sm">
          <span className="font-medium">{phrase("SPF include")}</span>
          <input
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
            value={include}
            onChange={(e) => setInclude(e.target.value)}
            placeholder="spf.provider.example"
          />
        </label>
        <label className="grid gap-1.5 text-sm">
          <span className="font-medium">{phrase("DKIM selector")}</span>
          <input
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
            value={selector}
            onChange={(e) => setSelector(e.target.value)}
            placeholder="s1"
          />
        </label>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {phrase("The DKIM public key for each domain is entered on that domain's page.")}
      </p>
      <Button
        className="mt-3"
        variant="outline"
        disabled={saving}
        onClick={async () => {
          setSaving(true);
          const r = await run(
            () =>
              admin.send(`/installation/transport-profiles/${profileId}/sending-facts`, "PUT", {
                egress_ips: ips
                  .split(/[\s,]+/)
                  .map((x) => x.trim())
                  .filter(Boolean),
                spf_include: include,
                dkim_selector: selector,
              }),
            phrase("Saved. Check each domain again to see the new requirements."),
          );
          setSaving(false);
          if (r) client.invalidateQueries({ queryKey: installationKey });
        }}
      >
        {saving ? phrase("Saving…") : phrase("Save sending facts")}
      </Button>
    </Section>
  );
}

export function useInstallation() {
  return useQuery({
    queryKey: installationKey,
    queryFn: () => admin.get<InstallationState>("/installation"),
    refetchInterval: 60_000,
  });
}

// Plain-text status labels: colour and icons are never the only signal.
export const statusLabels: Record<StepStatus, string> = {
  not_started: "Not started",
  checking: "Checking",
  ready: "Ready",
  needs_action: "Needs action",
  blocked_external_action: "Action needed outside Njiwa-Mail",
  error: "Error",
  skipped_optional: "Optional, skipped",
  stale: "Out of date",
};

// What to do about each stable problem code. Unknown codes fall back to the
// server's safe message.
export const problemFixes: Record<string, string> = {
  TRANSPORT_NOT_SELECTED: "Choose how this installation sends mail.",
  SMTP_CONFIG_INCOMPLETE:
    "Enter the SMTP host, port, encryption, username and password, then save.",
  CHECK_NOT_RUN: "Run the check.",
  CHECK_STALE: "The settings changed or the result is old. Run the check again.",
  CHECK_FAILED: "Fix the problem shown in the test result, then run the check again.",
  INSTALLER_NOT_AVAILABLE:
    "Run the host installer on this server as root: njiwa-mail installer plan, then installer apply -approve <digest> -record. Until then, mail cannot be sent directly from this VPS.",
  SMTP_EGRESS_BLOCKED:
    "Outbound port 25 is blocked. Ask your VPS provider to open it, or connect an existing mail server instead.",
  SMTP_DNS_FAILED: "The host name does not resolve. Check the spelling, or its DNS record.",
  SMTP_TCP_FAILED:
    "Could not connect. Check the port, and that a firewall allows this server to reach it.",
  SMTP_TLS_FAILED:
    "TLS failed. Use port 587 with STARTTLS or port 465 with TLS, and check the server's certificate matches its host name.",
  SMTP_LOGIN_FAILED: "The login was rejected. Check the username and enter the password again.",
  SMTP_AUTH_FAILED: "The login was rejected. Check the username and enter the password again.",
  SMTP_SENDER_FAILED:
    "This login may not send as the test sender. Allow it on your mail server, or use another sender.",
  SMTP_DNS_NOT_FOUND: "The host name does not resolve. Check the spelling, or its DNS record.",
  SMTP_HOST_FORBIDDEN:
    "The host resolves to a link-local or metadata address. Enter the mail server's real host name.",
  SMTP_TCP_REFUSED:
    "Nothing is listening on that port. Check the port number and that the mail server is running.",
  SMTP_TCP_TIMEOUT:
    "No answer on that port. A firewall or your hosting provider may block it; ask them to allow it.",
  SMTP_CONNECTION_FAILED:
    "Could not connect. Check the port, and that a firewall allows this server to reach it.",
  SMTP_TLS_MODE_MISMATCH:
    "The encryption setting does not match the port. Use STARTTLS with port 587, or TLS with port 465.",
  SMTP_STARTTLS_UNSUPPORTED:
    "The server does not offer STARTTLS on this port. Use port 465 with TLS, or enable STARTTLS on the server.",
  SMTP_TLS_CERT_INVALID:
    "The server's certificate is not trusted or does not match its host name. Use the host name on the certificate.",
  BOUNCES_NOT_CONFIGURED:
    "Set BOUNCE_ADDRESS to an address on a domain whose MX reaches Njiwa-Mail (your mail server relays it to the inbound listener), and run the inbound role. Sending works without it, but hard bounces are not suppressed automatically.",
  BOUNCE_LISTENER_DOWN:
    "Start the inbound role (docker compose --profile inbound up -d) so bounce reports can be received.",
  CLIENT_CERT_EXPIRING:
    "On the mail host run the installer again (njiwa-mail installer plan, then installer apply -record with the new digest). It renews the certificate and records it; mail keeps flowing during the change.",
  CLIENT_CERT_EXPIRED:
    "On the mail host run njiwa-mail installer plan -renew-certs, then installer apply -record with the new digest. Live mail stays queued until the new certificate is recorded.",
  TRANSPORT_FAILING:
    "Live mail is not reaching the mail server right now. Messages stay queued and are retried, nothing is lost. Run the connection test to see what is wrong.",
  SMTP_SERVER_TEMP_REFUSED:
    "The server is temporarily refusing mail. On a managed host this usually means the DKIM signer is stopped: start opendkim (systemctl start opendkim) and test again.",
  SMTP_NO_GREETING: "The server did not answer like a mail server. Check the host and port.",
  SMTP_AUTH_MISSING: "Enter the SMTP username and password.",
  SMTP_AUTH_REJECTED: "The login was rejected. Check the username and enter the password again.",
  SMTP_SENDER_REJECTED:
    "This login may not send as the test sender. Allow it on your mail server, or use another sender.",
  SMTP_OUTCOME_UNKNOWN:
    "The server may or may not have accepted the test. Check its logs before testing again; nothing is resent automatically.",
  HOST_OS_UNSUPPORTED:
    "Use Debian 12 or Ubuntu 22.04/24.04 for managed delivery, or connect an existing mail server.",
  HOST_NO_SYSTEMD: "Managed delivery needs systemd to run the mail service.",
  HOST_DISK_LOW: "Free at least 2 GiB and 100,000 inodes for the mail queue and logs.",
  HOST_MEMORY_LOW: "Provide at least 512 MiB of free memory.",
  HOST_CLOCK_NOT_SYNCED: "Enable NTP time synchronisation (timedatectl set-ntp true).",
  HOST_IP_NOT_LOCAL:
    "The sending IP is not on this host. Check OUTBOUND_IP, or confirm with your provider that mail leaves from it.",
  HOSTNAME_NO_ADDRESS: "Add an A record for the mail hostname pointing at the sending IP.",
  PTR_MISSING:
    "In your VPS provider's panel, set the reverse DNS (PTR) of the sending IP to the mail hostname.",
  PTR_MISMATCH:
    "The reverse DNS names another host. Change the PTR of the sending IP to the mail hostname.",
  FCRDNS_FAILED: "The mail hostname must resolve back to the sending IP. Fix its A record.",
  MTA_CONFLICT:
    "Another mail server runs on this host. Remove it, or connect it as an existing mail server instead.",
  PREFLIGHT_PARAMETERS_MISSING: "Set MAIL_HOSTNAME and OUTBOUND_IP, then run the preflight again.",
  SMTP_TLS_VERIFICATION_FAILED:
    "The server's certificate is not trusted or does not match its host name. Use the host name on the certificate.",
};

function StatusIcon({ status }: { status: StepStatus }) {
  const cls = "mt-0.5 shrink-0";
  if (status === "ready")
    return <CheckCircle2 size={18} className={cls + " text-success"} aria-hidden />;
  if (status === "stale" || status === "checking")
    return <Clock size={18} className={cls + " text-warning"} aria-hidden />;
  if (status === "error" || status === "blocked_external_action")
    return <XCircle size={18} className={cls + " text-destructive"} aria-hidden />;
  if (status === "needs_action")
    return <AlertTriangle size={18} className={cls + " text-warning"} aria-hidden />;
  return <Circle size={18} className={cls + " text-muted-foreground"} aria-hidden />;
}

/** Exactly two delivery modes, as an accessible radio group. */
export function DeliveryModeCard({
  value,
  onChange,
  disabled,
}: {
  value: DeliveryMode | "";
  onChange: (m: DeliveryMode) => void;
  disabled?: boolean;
}) {
  const phrase = usePhrase();
  const options: { mode: DeliveryMode; title: string; body: string }[] = [
    {
      mode: "managed_mta",
      title: "Send from my VPS",
      body: "Njiwa-Mail helps configure a local mail delivery service on this server. Needs outbound port 25, a public IP, DNS and PTR (reverse DNS) access.",
    },
    {
      mode: "existing_smtp",
      title: "Connect an existing mail server",
      body: "Use a mail server you already run, such as Postfix, Exim or Mailcow, over authenticated SMTP. No new mail server is installed.",
    },
  ];
  return (
    <fieldset className="grid gap-3 sm:grid-cols-2" disabled={disabled}>
      <legend className="mb-2 text-sm font-semibold">{phrase("How will you send email?")}</legend>
      {options.map((o) => (
        <label
          key={o.mode}
          className={
            "flex cursor-pointer gap-3 rounded-xl border bg-card p-4 transition focus-within:ring-2 focus-within:ring-ring " +
            (value === o.mode ? "border-primary" : "hover:border-primary/60")
          }
        >
          <input
            type="radio"
            name="delivery-mode"
            value={o.mode}
            checked={value === o.mode}
            onChange={() => onChange(o.mode)}
            className="mt-1"
          />
          <span>
            <span className="block font-semibold">{phrase(o.title)}</span>
            <span className="mt-1 block text-sm text-muted-foreground">{phrase(o.body)}</span>
          </span>
        </label>
      ))}
    </fieldset>
  );
}

/** Steps with plain status text, the fix for each problem and when it was checked. */
export function ReadinessSteps({
  readiness,
  onRecheck,
  rechecking,
}: {
  readiness: Readiness;
  onRecheck?: () => void;
  rechecking?: boolean;
}) {
  const phrase = usePhrase();
  const { complete, required } = readiness.progress;
  return (
    <div>
      <div className="mb-3 flex items-center gap-3">
        <div
          className="h-2 flex-1 overflow-hidden rounded-full bg-muted"
          role="progressbar"
          aria-label={phrase("Installation setup")}
          aria-valuemin={0}
          aria-valuemax={required}
          aria-valuenow={complete}
        >
          <div
            className="h-full bg-primary transition-all"
            style={{ width: required ? `${(complete / required) * 100}%` : "0%" }}
          />
        </div>
        <span className="text-sm text-muted-foreground">
          {complete}/{required} {phrase("required steps")}
        </span>
      </div>
      <ol className="space-y-2" aria-live="polite">
        {readiness.steps.map((st) => {
          const fix = st.problem_code ? problemFixes[st.problem_code] : undefined;
          return (
            <li
              key={st.id}
              className="flex items-start gap-3 rounded-lg border p-3"
              data-step={st.id}
            >
              <StatusIcon status={st.status} />
              <div className="min-w-0 flex-1 text-sm">
                <p className="font-medium">
                  {phrase(st.title)}{" "}
                  {!st.required && (
                    <span className="text-xs font-normal text-muted-foreground">
                      ({phrase("optional")}){" "}
                    </span>
                  )}
                  <span
                    className="text-xs font-normal text-muted-foreground"
                    data-status={st.status}
                  >
                    — {phrase(statusLabels[st.status] ?? st.status)}
                  </span>
                </p>
                {st.status !== "ready" && (fix || st.message) && (
                  <p className="mt-1 text-xs">{phrase(fix ?? st.message ?? "")}</p>
                )}
                {st.problem_code && st.status !== "ready" && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {phrase("Code")}: <code>{st.problem_code}</code>
                  </p>
                )}
                {st.checked_at && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {phrase("Last checked")} <Time value={st.checked_at} relative />
                  </p>
                )}
              </div>
            </li>
          );
        })}
      </ol>
      {onRecheck && (
        <Button
          className="mt-3"
          variant="outline"
          size="sm"
          disabled={rechecking}
          onClick={onRecheck}
        >
          {rechecking ? phrase("Checking…") : phrase("Run checks again")}
        </Button>
      )}
    </div>
  );
}

const welcomeText: Record<InstallationState["welcome_state"], string> = {
  ready_to_configure: "Choose how this installation will send email to get started.",
  legacy_detected:
    "Existing SMTP settings were found from an earlier version. Review them and continue with the existing installation; nothing changes until you save.",
  in_progress: "Setup is under way. Complete the steps below.",
  degraded:
    "Mail delivery was working but is not ready now. Live mail stays queued; fix the step below and check again.",
  completed:
    "Mail delivery is ready. Each project still needs its own sending domain and sender before it can send live mail.",
};

/**
 * The installation panel for Settings › Delivery infrastructure: welcome
 * state, mode choice (only when nothing is active) and readiness steps.
 */
export function DeliveryInfrastructure({
  onRecheck,
  rechecking,
}: {
  onRecheck?: () => void;
  rechecking?: boolean;
}) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useInstallation();
  const [choice, setChoice] = useState<DeliveryMode | "">("");
  const [saving, setSaving] = useState(false);
  const confirm = async (mode: DeliveryMode) => {
    setSaving(true);
    const r = await run(
      () => admin.send<InstallationState>("/installation/wizard", "PUT", { selected_mode: mode }),
      phrase("Delivery mode saved."),
    );
    setSaving(false);
    if (r) {
      client.setQueryData(installationKey, r);
      client.invalidateQueries({ queryKey: ["ws"] });
    }
  };
  return (
    <DataState q={q} rows={3}>
      {(st) => {
        const mode = st.readiness.mode;
        const chosen = choice || st.wizard.selected_mode || "";
        const canChoose = !st.active_profile;
        return (
          <Section title={phrase("Delivery infrastructure")}>
            <p
              role="status"
              className={
                "mb-4 rounded-lg border p-3 text-sm " +
                (st.welcome_state === "degraded"
                  ? "border-destructive/40 bg-destructive/10"
                  : st.welcome_state === "completed"
                    ? "border-success/40 bg-success/10"
                    : "border-primary/40 bg-primary/10")
              }
            >
              {phrase(welcomeText[st.welcome_state])}
            </p>
            {st.welcome_state === "legacy_detected" && (
              <Button className="mb-4" disabled={saving} onClick={() => confirm("existing_smtp")}>
                {phrase("Continue existing installation")}
              </Button>
            )}
            {canChoose && (
              <div className="mb-4 space-y-3">
                <DeliveryModeCard value={chosen} onChange={setChoice} disabled={saving} />
                <Button
                  disabled={saving || !chosen || chosen === st.wizard.selected_mode}
                  onClick={() => chosen && confirm(chosen)}
                >
                  {phrase("Continue")}
                </Button>
              </div>
            )}
            {!canChoose && st.active_profile && (
              <p className="mb-3 text-sm text-muted-foreground">
                {phrase("Active mode")}:{" "}
                <span className="font-medium text-foreground">
                  {phrase(
                    st.active_profile.mode === "managed_mta"
                      ? "Send from my VPS"
                      : "Connect an existing mail server",
                  )}
                </span>{" "}
                · {phrase("settings revision")} {st.active_profile.config_revision}.{" "}
                {phrase(
                  "Changing mode can affect queued messages; it goes through a planned switch, not this page.",
                )}
              </p>
            )}
            {st.environment_override && (
              <p className="mb-3 rounded-lg border border-warning/40 bg-warning/10 p-3 text-xs">
                {phrase(
                  "SMTP_HOST is set in the server environment and takes precedence over the saved settings. A test only counts for the saved settings when both are identical.",
                )}
              </p>
            )}
            {mode === "managed_mta" && (
              <div className="mb-3 rounded-lg border p-3 text-sm">
                <p>
                  {phrase(
                    "The host checks run on the server itself and change nothing. Run this on the VPS, then reload this page:",
                  )}
                </p>
                <pre className="mt-2 overflow-x-auto rounded bg-muted p-2 text-xs">
                  <code>njiwa-mail preflight -record</code>
                </pre>
              </div>
            )}
            {mode && (
              <ReadinessSteps
                readiness={st.readiness}
                onRecheck={mode === "existing_smtp" ? onRecheck : undefined}
                rechecking={rechecking}
              />
            )}
          </Section>
        );
      }}
    </DataState>
  );
}
