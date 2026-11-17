import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { admin } from "@/lib/admin-api";
import { CheckCircle2, Pencil, RefreshCw, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import {
  Field,
  inputClass,
  selectClass,
  Section,
  errorMessage,
  type Row,
} from "@/features/workspace/ui";
import { installationKey } from "./Installation";

type Step = {
  key: string;
  title: string;
  ok: boolean;
  skipped?: boolean;
  detail: string;
  code?: string;
};
type Connection = { ok: boolean; saved: boolean; steps: Step[] };

export function ExistingMailServerSetup() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const cfg = useQuery({
    queryKey: ["mail-server"],
    queryFn: () => admin.get<Row>("/mail-server"),
  });
  const [form, setForm] = useState({
    smtp_host: "",
    smtp_username: "",
    smtp_password: "",
    provider_type: "mailcow",
    smtp_port: 587,
    smtp_tls_mode: "starttls",
    test_sender: "",
  });
  const [result, setResult] = useState<Connection | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [recipient, setRecipient] = useState("");
  const [sent, setSent] = useState<Row | null>(null);
  const [editing, setEditing] = useState(false);
  const [retesting, setRetesting] = useState(false);
  useEffect(() => {
    if (!cfg.data) return;
    const d = cfg.data;
    setForm((f) => ({
      ...f,
      smtp_host: String(d.smtp_host || ""),
      smtp_username: String(d.smtp_username || ""),
      smtp_password: "",
      provider_type: String(d.provider_type || "mailcow"),
      smtp_port: Number(d.smtp_port || 587),
      smtp_tls_mode: String(d.smtp_tls_mode || "starttls"),
      test_sender: String(d.test_sender || ""),
    }));
  }, [cfg.data]);
  function update<K extends keyof typeof form>(key: K, value: (typeof form)[K]) {
    setForm((f) => ({ ...f, [key]: value }));
    setResult(null);
    setSent(null);
    setError("");
  }
  async function refresh() {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["mail-server"] }),
      client.invalidateQueries({ queryKey: installationKey }),
      client.invalidateQueries({ queryKey: ["ws"] }),
    ]);
  }
  const controlled = !!cfg.data?.environment_smtp;
  const lastTest = parseLastTest(cfg.data?.last_test);
  // A saved server whose last check passed is connected: show it instead of
  // asking for the same details again. The form stays one click away.
  const connected = !!cfg.data?.smtp_host && !!cfg.data?.smtp_password_set && lastTest?.ok === true;
  const showForm = !connected || editing;
  async function retest() {
    setRetesting(true);
    setError("");
    try {
      // The check records its result; a failure turns this page back into the form.
      const r = await admin.send<{ ok: boolean }>("/mail-server/test", "POST");
      if (!r.ok) setError(phrase("The connection check failed. Review the settings below."));
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setRetesting(false);
    }
  }
  return (
    <div id="delivery-connection" className="scroll-mt-24 space-y-4">
      {connected && cfg.data && (
        <ConnectedCard
          data={cfg.data}
          steps={lastTest?.detail ?? []}
          checkedAt={cfg.data.last_test_at ? String(cfg.data.last_test_at) : ""}
          retesting={retesting}
          onRetest={retest}
          editing={editing}
          onEdit={() => {
            setEditing((v) => !v);
            setResult(null);
          }}
        />
      )}
      {showForm && (
        <Section
          title={phrase(connected ? "Change mail server settings" : "Connect Existing Mail Server")}
        >
          <p className="mb-4 text-sm text-muted-foreground">
            {phrase(
              "Use a mailbox you already have. Njiwa-Mail connects securely to your server; no new VPS or Mailcow installation is needed.",
            )}
          </p>
          {cfg.error && <p role="alert">{errorMessage(cfg.error)}</p>}
          {controlled && (
            <p role="status">
              {phrase(
                "SMTP is controlled by the server environment. Update that configuration before connecting another server.",
              )}
            </p>
          )}
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError("");
              setSent(null);
              try {
                const body = {
                  ...form,
                  smtp_password: form.smtp_password || undefined,
                  test_sender: form.test_sender || undefined,
                };
                const r = await admin.send<Connection>("/mail-server/connect", "POST", body);
                setResult(r);
                if (r.saved) {
                  await refresh();
                  setEditing(false);
                }
              } catch (err) {
                setError(errorMessage(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <fieldset disabled={busy || controlled || !cfg.data} className="space-y-4">
              <div className="flex gap-4" role="group" aria-label={phrase("Server type")}>
                {[
                  ["mailcow", "Mailcow"],
                  ["generic", "Generic SMTP"],
                ].map(([value, label]) => (
                  <label key={value} className="flex items-center gap-2">
                    <input
                      type="radio"
                      name="smtp-provider"
                      value={value}
                      checked={form.provider_type === value}
                      onChange={() => update("provider_type", value)}
                    />
                    {phrase(label)}
                  </label>
                ))}
              </div>
              <Field
                label={phrase("SMTP hostname")}
                hint={phrase(
                  "For example mail.example.com. Use the hostname on your mail server's certificate.",
                )}
              >
                <input
                  required
                  className={inputClass}
                  value={form.smtp_host}
                  onChange={(e) => update("smtp_host", e.target.value.trim())}
                />
              </Field>
              <Field
                label={phrase("Mailbox username")}
                hint={phrase(
                  "Usually your full mailbox email address. This is also the default sender.",
                )}
              >
                <input
                  required
                  autoComplete="username"
                  className={inputClass}
                  value={form.smtp_username}
                  onChange={(e) => update("smtp_username", e.target.value)}
                />
              </Field>
              <Field
                label={phrase("Mailbox password")}
                hint={
                  cfg.data?.smtp_password_set
                    ? phrase(
                        "Leave blank to reuse the saved password for the same host and username.",
                      )
                    : phrase("Stored encrypted. Never displayed again.")
                }
              >
                <input
                  required={!cfg.data?.smtp_password_set}
                  type="password"
                  autoComplete="new-password"
                  className={inputClass}
                  value={form.smtp_password}
                  onChange={(e) => update("smtp_password", e.target.value)}
                />
              </Field>
              <p className="text-sm text-muted-foreground">
                {phrase(
                  "Port 587 with STARTTLS is selected by default. TLS certificate verification is always enabled.",
                )}
              </p>
              <details className="rounded-lg border p-3">
                <summary className="cursor-pointer font-medium">
                  {phrase("Advanced connection settings")}
                </summary>
                <div className="mt-3 space-y-3">
                  <Field label={phrase("SMTP port")}>
                    <input
                      required
                      type="number"
                      min={1}
                      max={65535}
                      className={inputClass}
                      value={form.smtp_port}
                      onChange={(e) => update("smtp_port", Number(e.target.value))}
                    />
                  </Field>
                  <Field label={phrase("TLS mode")}>
                    <select
                      className={selectClass}
                      value={form.smtp_tls_mode}
                      onChange={(e) => update("smtp_tls_mode", e.target.value)}
                    >
                      <option value="starttls">STARTTLS (usually 587)</option>
                      <option value="implicit">TLS (usually 465)</option>
                    </select>
                  </Field>
                  <Field
                    label={phrase("Authorized sender override")}
                    hint={phrase("Only needed if the sender differs from your mailbox username.")}
                  >
                    <input
                      type="email"
                      className={inputClass}
                      value={form.test_sender}
                      onChange={(e) => update("test_sender", e.target.value)}
                    />
                  </Field>
                </div>
              </details>
              <div className="flex flex-wrap gap-2">
                <Button type="submit">
                  {busy ? phrase("Checking connection…") : phrase("Connect and test")}
                </Button>
                {connected && (
                  <Button type="button" variant="outline" onClick={() => setEditing(false)}>
                    {phrase("Cancel")}
                  </Button>
                )}
              </div>
            </fieldset>
          </form>
          {error && (
            <p role="alert" className="mt-3 text-destructive">
              {error}
            </p>
          )}
          {result && (
            <div className="mt-4" role="status">
              <p className="font-medium">
                {result.saved
                  ? phrase("Connected. Send a test email below.")
                  : phrase("Connection failed. Your saved settings have not changed.")}
              </p>
              <ol className="mt-3 space-y-3">
                {result.steps.map((step) => (
                  <li key={step.key}>
                    <strong>
                      {step.ok ? "✓" : step.skipped ? "—" : "✕"} {phrase(step.title)}
                    </strong>
                    <p className="text-sm text-muted-foreground">{step.detail}</p>
                    {step.code && <code className="text-xs">{step.code}</code>}
                  </li>
                ))}
              </ol>
            </div>
          )}
        </Section>
      )}
      {(result?.saved || connected) && (
        <Section title={phrase("Send a test email")}>
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError("");
              try {
                setSent(await admin.send<Row>("/mail-server/test-send", "POST", { to: recipient }));
                await refresh();
              } catch (err) {
                setError(errorMessage(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <Field label={phrase("Test recipient")}>
              <input
                required
                type="email"
                className={inputClass}
                value={recipient}
                onChange={(e) => setRecipient(e.target.value)}
              />
            </Field>
            <Button type="submit" disabled={busy}>
              {phrase("Send test email")}
            </Button>
          </form>
          {sent && (
            <p role="status" className="mt-3">
              {sent.state === "submitted"
                ? phrase(
                    "Your mail server accepted the test email. Check the recipient's inbox and spam folder; acceptance does not guarantee delivery.",
                  )
                : `${String(sent.state)}: ${String(sent.reason || "")} ${String(sent.reply || "")}`}
            </p>
          )}
        </Section>
      )}
      <details className="rounded-lg border p-4">
        <summary className="cursor-pointer font-medium">
          {phrase("Optional next step: inbound mail and bounces")}
        </summary>
        <p className="mt-3 text-sm">
          {phrase(
            "Outbound sending works without an inbound listener or bounce address. For an existing Mailcow mailbox, the supported IMAP poller can ingest messages over verified TLS on port 993 without changing Postfix. An operator can configure IMAP_HOST, IMAP_USERNAME, IMAP_PASSWORD and IMAP_MAILBOX on the Njiwa inbound service. Use a dedicated mailbox: successfully ingested messages are marked as read. Bounce processing requires reports to reach that mailbox.",
          )}
        </p>
      </details>
    </div>
  );
}

type LastTest = { ok: boolean; kind?: string; detail?: Step[] };

function parseLastTest(v: unknown): LastTest | null {
  if (!v) return null;
  if (typeof v === "string") {
    try {
      return JSON.parse(v) as LastTest;
    } catch {
      return null;
    }
  }
  return v as LastTest;
}

function ConnectedCard({
  data,
  steps,
  checkedAt,
  retesting,
  onRetest,
  editing,
  onEdit,
}: {
  data: Row;
  steps: Step[];
  checkedAt: string;
  retesting: boolean;
  onRetest: () => void;
  editing: boolean;
  onEdit: () => void;
}) {
  const phrase = usePhrase();
  const tls = data.smtp_tls_mode === "implicit" ? "TLS" : "STARTTLS";
  const rows: [string, string][] = [
    [phrase("SMTP server"), `${String(data.smtp_host)}:${String(data.smtp_port)} · ${tls}`],
    [phrase("Mailbox username"), String(data.smtp_username || "")],
    [phrase("Default sender"), String(data.test_sender || data.smtp_username || "")],
    [phrase("Server type"), data.provider_type === "mailcow" ? "Mailcow" : phrase("Generic SMTP")],
  ];
  return (
    <section
      className="rounded-xl border border-success/40 bg-success/5 p-4 sm:p-5"
      data-testid="mail-server-connected"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <span className="grid h-10 w-10 shrink-0 place-items-center rounded-full bg-success/15 text-success">
            <CheckCircle2 size={22} aria-hidden />
          </span>
          <div>
            <h2 className="font-semibold">{phrase("Mail server connected")}</h2>
            <p className="text-sm text-muted-foreground">
              {phrase("Njiwa-Mail sends through this server. You do not need to connect it again.")}
            </p>
            {checkedAt && (
              <p className="mt-1 text-xs text-muted-foreground">
                {phrase("Last checked")}: {new Date(checkedAt).toLocaleString()}
              </p>
            )}
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" disabled={retesting} onClick={onRetest}>
            <RefreshCw size={14} className={retesting ? "animate-spin" : undefined} />
            {retesting ? phrase("Checking connection…") : phrase("Check again")}
          </Button>
          <Button size="sm" variant={editing ? "secondary" : "outline"} onClick={onEdit}>
            <Pencil size={14} /> {phrase("Change settings")}
          </Button>
        </div>
      </div>
      <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
        {rows.map(([k, v]) => (
          <div key={k} className="rounded-lg border bg-card px-3 py-2">
            <dt className="text-xs text-muted-foreground">{k}</dt>
            <dd className="truncate font-medium">{v}</dd>
          </div>
        ))}
      </dl>
      {steps.length > 0 && (
        <ul className="mt-4 flex flex-wrap gap-2">
          {steps.map((st) => (
            <li
              key={st.key}
              title={st.detail}
              className="inline-flex items-center gap-1 rounded-full border bg-card px-2.5 py-1 text-xs"
            >
              {st.ok ? (
                <CheckCircle2 size={13} className="text-success" aria-hidden />
              ) : (
                <XCircle size={13} className="text-destructive" aria-hidden />
              )}
              {phrase(st.title)}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
