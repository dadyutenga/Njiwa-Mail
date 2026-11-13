import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, MinusCircle, Plus, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { useAuth } from "@/features/auth/auth";
import { admin, getCsrf } from "@/lib/admin-api";
import { toast } from "sonner";
import {
  DeliveryInfrastructure,
  installationKey,
  SendingFactsForm,
  useInstallation,
} from "@/features/installation/Installation";
import { DiscoveryPanel, SetupJourneyPanel } from "@/features/installation/Setup";
import { TransportProfiles } from "@/features/installation/Profiles";
import { useWorkspace, useWsKey, type ProjectRow } from "../context";
import { NewProjectDialog } from "../Layout";
import {
  ConfirmButton,
  DataState,
  Empty,
  emailOk,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  Pager,
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  Table,
  Time,
  useOffset,
  useParam,
  type Page,
  type Row,
} from "../ui";

// ---------------------------------------------------------------- projects

export function Projects() {
  const phrase = usePhrase();
  const { project, to } = useWorkspace();
  const client = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<ProjectRow | null>(null);
  const offset = useOffset();
  const q = useQuery({
    queryKey: ["projects", offset],
    queryFn: () => admin.get<Page<ProjectRow>>(`/projects?limit=50&offset=${offset}`),
  });
  return (
    <>
      <PageHeader
        title={phrase("Projects")}
        description={phrase(
          "Each project has its own domains, senders, keys and mail, in a Sandbox and a Live environment.",
        )}
        action={
          <Button onClick={() => setCreating(true)}>
            <Plus size={16} /> {phrase("New project")}
          </Button>
        }
      />
      <DataState q={q}>
        {(d) => (
          <>
            <Table
              rows={d.items as unknown as Row[]}
              rowKey={(r) => s(r.id)}
              columns={[
                {
                  key: "name",
                  label: phrase("Project"),
                  render: (r) => (
                    <span className="flex items-center gap-2">
                      <Link
                        className="font-medium hover:underline"
                        to={`/p/${encodeURIComponent(s(r.name))}/dashboard`}
                      >
                        {s(r.name)}
                      </Link>
                      {r.name === project && (
                        <StateBadge state="active" label={phrase("current")} />
                      )}
                    </span>
                  ),
                },
                {
                  key: "enabled",
                  label: phrase("State"),
                  render: (r) => (
                    <StateBadge
                      state={r.enabled ? "active" : "cancelled"}
                      label={r.enabled ? phrase("enabled") : phrase("disabled")}
                    />
                  ),
                },
                {
                  key: "limits",
                  label: phrase("Send limits"),
                  render: (r) =>
                    `${s(r.send_per_minute)}/min · ${s(r.send_per_day)}/${phrase("day")}`,
                },
                {
                  key: "created_at",
                  label: phrase("Created"),
                  render: (r) => <Time value={r.created_at} />,
                },
                {
                  key: "x",
                  label: <span className="sr-only">{phrase("Actions")}</span>,
                  render: (r) => (
                    <span className="flex gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setEditing(r as unknown as ProjectRow)}
                      >
                        {phrase("Limits")}
                      </Button>
                      <Button size="sm" variant="outline" asChild>
                        <Link to={to(`projects/${encodeURIComponent(s(r.name))}`)}>
                          {phrase("Environments")}
                        </Link>
                      </Button>
                      <ConfirmButton
                        destructive={!!r.enabled}
                        title={
                          r.enabled
                            ? `${phrase("Disable")} ${s(r.name)}?`
                            : `${phrase("Enable")} ${s(r.name)}?`
                        }
                        description={
                          r.enabled
                            ? phrase(
                                "Applications of this project can no longer send. Queued mail stays queued.",
                              )
                            : phrase("The project can send again.")
                        }
                        confirm={r.enabled ? "Disable" : "Enable"}
                        onConfirm={async () => {
                          await run(
                            () =>
                              admin.send(`/projects/${encodeURIComponent(s(r.name))}`, "PATCH", {
                                enabled: !r.enabled,
                                send_per_minute: Number(r.send_per_minute),
                                send_per_day: Number(r.send_per_day),
                                recipient_per_minute: Number(r.recipient_per_minute),
                                recipient_per_hour: Number(r.recipient_per_hour),
                              }),
                            r.enabled ? phrase("Project disabled.") : phrase("Project enabled."),
                          );
                          client.invalidateQueries({ queryKey: ["projects"] });
                        }}
                      >
                        {r.enabled ? phrase("Disable") : phrase("Enable")}
                      </ConfirmButton>
                    </span>
                  ),
                },
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
      <NewProjectDialog open={creating} onOpenChange={setCreating} />
      {editing && <LimitsDialog project={editing} onClose={() => setEditing(null)} />}
    </>
  );
}

function LimitsDialog({ project, onClose }: { project: ProjectRow; onClose: () => void }) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [v, setV] = useState({
    send_per_minute: String(project.send_per_minute),
    send_per_day: String(project.send_per_day),
    recipient_per_minute: String(project.recipient_per_minute),
    recipient_per_hour: String(project.recipient_per_hour),
  });
  const [error, setError] = useState("");
  const bad = Object.values(v).some((x) => !/^\d+$/.test(x));
  const labels: Record<keyof typeof v, string> = {
    send_per_minute: "Messages per minute",
    send_per_day: "Messages per day",
    recipient_per_minute: "Per recipient per minute",
    recipient_per_hour: "Per recipient per hour",
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>
          {phrase("Send limits for")} {project.name}
        </DialogTitle>
        <DialogDescription>
          {phrase(
            "Requests above a limit get HTTP 429 with Retry-After. Bulk announcements have their own limits.",
          )}
        </DialogDescription>
        <form
          className="grid gap-3 sm:grid-cols-2"
          onSubmit={async (e) => {
            e.preventDefault();
            if (bad) return setError(phrase("Whole numbers only."));
            try {
              await admin.send(
                `/projects/${encodeURIComponent(project.name)}`,
                "PATCH",
                Object.fromEntries(Object.entries(v).map(([k, x]) => [k, Number(x)])),
              );
              client.invalidateQueries({ queryKey: ["projects"] });
              onClose();
            } catch (err) {
              setError(errorMessage(err));
            }
          }}
        >
          {(Object.keys(v) as (keyof typeof v)[]).map((k) => (
            <Field
              key={k}
              label={phrase(labels[k])}
              error={!/^\d+$/.test(v[k]) ? phrase("Whole number") : undefined}
            >
              <input
                className={inputClass}
                inputMode="numeric"
                value={v[k]}
                onChange={(e) => setV({ ...v, [k]: e.target.value })}
              />
            </Field>
          ))}
          {error && (
            <p role="alert" className="text-sm text-destructive sm:col-span-2">
              {error}
            </p>
          )}
          <Button type="submit" disabled={bad} className="sm:col-span-2">
            {phrase("Save limits")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- users

export function Users() {
  const phrase = usePhrase();
  const { user } = useAuth();
  const client = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [operator, setOperator] = useState(true);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: ["users"],
    queryFn: () => admin.get<Page>("/users?limit=100"),
  });
  const refresh = () => client.invalidateQueries({ queryKey: ["users"] });
  return (
    <>
      <PageHeader
        title={phrase("Users")}
        description={phrase(
          "People who can sign in. Installation operators also manage delivery infrastructure, domain ownership, users and settings.",
        )}
      />
      <form
        className="mb-4 grid gap-3 rounded-xl border bg-card p-4 sm:grid-cols-[1fr_1fr_auto]"
        onSubmit={async (e) => {
          e.preventDefault();
          if (!emailOk(email)) return setError(phrase("Enter an email address."));
          if (password.length < 12)
            return setError(phrase("The temporary password needs at least 12 characters."));
          setError("");
          if (
            await run(
              () =>
                admin.send("/users", "POST", {
                  email,
                  password,
                  installation_operator: operator,
                }),
              phrase("User added."),
            )
          ) {
            setEmail("");
            setPassword("");
            refresh();
          }
        }}
      >
        <Field label={phrase("Email")} error={error}>
          <input
            className={inputClass}
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>
        <Field
          label={phrase("Temporary password")}
          hint={phrase("12+ characters; share it privately.")}
        >
          <input
            className={inputClass}
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <label className="flex items-center gap-2 text-sm sm:col-span-2">
          <input
            type="checkbox"
            checked={operator}
            onChange={(e) => setOperator(e.target.checked)}
          />
          {phrase("Installation operator (can change delivery infrastructure, users and settings)")}
        </label>
        <Button type="submit" className="self-end">
          {phrase("Add user")}
        </Button>
      </form>
      <DataState q={q}>
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              { key: "email", label: phrase("Email") },
              {
                key: "installation_operator",
                label: phrase("Operator"),
                render: (r) => (r.installation_operator ? phrase("yes") : phrase("no")),
              },
              {
                key: "mfa_enabled",
                label: phrase("Two-step"),
                render: (r) => (r.mfa_enabled ? phrase("on") : phrase("off")),
              },
              {
                key: "disabled",
                label: phrase("State"),
                render: (r) => (
                  <StateBadge
                    state={r.disabled ? "cancelled" : "active"}
                    label={r.disabled ? phrase("disabled") : phrase("active")}
                  />
                ),
              },
              {
                key: "last_login_at",
                label: phrase("Last sign-in"),
                render: (r) => <Time value={r.last_login_at} relative />,
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) =>
                  r.email === user?.email ? (
                    <span className="text-xs text-muted-foreground">{phrase("you")}</span>
                  ) : (
                    <span className="flex gap-1">
                      <ConfirmButton
                        destructive={!r.disabled}
                        title={`${r.disabled ? phrase("Enable") : phrase("Disable")} ${s(r.email)}?`}
                        description={
                          r.disabled
                            ? phrase("They can sign in again.")
                            : phrase("They are signed out and cannot sign in.")
                        }
                        confirm={r.disabled ? "Enable" : "Disable"}
                        onConfirm={async () => {
                          await run(
                            () =>
                              admin.send(`/users/${s(r.id)}`, "PATCH", { disabled: !r.disabled }),
                            phrase("Saved."),
                          );
                          refresh();
                        }}
                      >
                        {r.disabled ? phrase("Enable") : phrase("Disable")}
                      </ConfirmButton>
                      <ConfirmButton
                        title={`${r.installation_operator ? phrase("Remove operator access from") : phrase("Make an installation operator:")} ${s(r.email)}?`}
                        description={phrase(
                          "They are signed out and sign in again with the new access.",
                        )}
                        confirm="Confirm"
                        onConfirm={async () => {
                          await run(
                            () =>
                              admin.send(`/users/${s(r.id)}`, "PATCH", {
                                installation_operator: !r.installation_operator,
                              }),
                            phrase("Saved."),
                          );
                          refresh();
                        }}
                      >
                        {r.installation_operator
                          ? phrase("Remove operator")
                          : phrase("Make operator")}
                      </ConfirmButton>
                      {!!r.mfa_enabled && (
                        <ConfirmButton
                          destructive
                          title={`${phrase("Reset two-step sign-in for")} ${s(r.email)}?`}
                          description={phrase(
                            "Use this when the device and recovery codes are lost. They are signed out and must set it up again.",
                          )}
                          confirm="Reset"
                          onConfirm={async () => {
                            await run(
                              () => admin.send(`/users/${s(r.id)}`, "PATCH", { reset_mfa: true }),
                              phrase("Saved."),
                            );
                            refresh();
                          }}
                        >
                          {phrase("Reset two-step")}
                        </ConfirmButton>
                      )}
                    </span>
                  ),
              },
            ]}
          />
        )}
      </DataState>
    </>
  );
}

// ---------------------------------------------------------------- mail server

type Step = { key: string; title: string; ok: boolean; skipped?: boolean; detail: string };

function StepIcon({ st }: { st: { ok: boolean; skipped?: boolean } }) {
  if (st.ok) return <CheckCircle2 size={16} className="text-success" aria-label="pass" />;
  if (st.skipped)
    return <MinusCircle size={16} className="text-muted-foreground" aria-label="not run" />;
  return <XCircle size={16} className="text-destructive" aria-label="fail" />;
}

export function MailServer() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const cfg = useQuery({
    queryKey: ["mail-server"],
    queryFn: () => admin.get<Row>("/mail-server"),
  });
  const status = useQuery({
    queryKey: ["mail-server", "status"],
    queryFn: () => admin.get<Row>("/mail-server/status"),
    refetchInterval: 30_000,
  });
  const [f, setF] = useState({
    smtp_host: "",
    smtp_port: "587",
    smtp_tls_mode: "starttls",
    smtp_username: "",
    smtp_password: "",
    test_sender: "",
    inbound_listener: "",
    mailcow_api_url: "",
    mailcow_api_key: "",
    provider_type: "generic",
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [test, setTest] = useState<{
    ok: boolean;
    steps: Step[];
    mailcow_api?: { ok: boolean; detail: string };
  } | null>(null);
  const [testing, setTesting] = useState(false);
  const [sendTo, setSendTo] = useState("");
  const [sendResult, setSendResult] = useState<Row | null>(null);
  const [queued, setQueued] = useState<string | null>(null);
  const { project, to: wsTo } = useWorkspace();
  useEffect(() => {
    const d = cfg.data;
    if (!d) return;
    setF((x) => ({
      ...x,
      smtp_host: s(d.smtp_host),
      smtp_port: s(d.smtp_port) || "587",
      smtp_tls_mode: s(d.smtp_tls_mode) || "starttls",
      smtp_username: s(d.smtp_username),
      test_sender: s(d.test_sender),
      inbound_listener: s(d.inbound_listener),
      mailcow_api_url: s(d.mailcow_api_url),
      smtp_password: "",
      mailcow_api_key: "",
      provider_type: s(d.provider_type) || "generic",
    }));
  }, [cfg.data]);
  const set = (k: keyof typeof f, v: string) => {
    setF({ ...f, [k]: v });
    const e = { ...errors };
    delete e[k];
    if (k === "smtp_port" && v && !/^\d+$/.test(v))
      e.smtp_port = phrase("A number, usually 587 or 465.");
    if (k === "test_sender" && v && !emailOk(v)) e.test_sender = phrase("An email address.");
    if (k === "mailcow_api_url" && v && !v.startsWith("https://"))
      e.mailcow_api_url = phrase("Must start with https://");
    if (k === "inbound_listener" && v && !/^[^:\s]+:\d+$/.test(v))
      e.inbound_listener = phrase("host:port, for example njiwa-inbound:2525");
    setErrors(e);
  };
  const env = cfg.data?.environment_smtp as Row | null | undefined;
  const save = async () => {
    if (Object.keys(errors).length) return;
    const body: Record<string, unknown> = { ...f, smtp_port: Number(f.smtp_port) };
    if (!f.smtp_password) delete body.smtp_password;
    if (!f.mailcow_api_key) delete body.mailcow_api_key;
    if (f.provider_type !== "mailcow") {
      body.mailcow_api_url = "";
      delete body.mailcow_api_key;
    }
    if (
      await run(
        () => admin.send("/mail-server", "PUT", body),
        phrase("Mail server settings saved. Run the connection test."),
      )
    ) {
      client.invalidateQueries({ queryKey: ["mail-server"] });
      client.invalidateQueries({ queryKey: installationKey });
      setTest(null);
    }
  };
  const runTest = async () => {
    setTesting(true);
    const r = await run(
      () => admin.send<NonNullable<typeof test>>("/mail-server/test", "POST"),
      "",
    );
    setTesting(false);
    if (r) setTest(r);
    client.invalidateQueries({ queryKey: ["mail-server"] });
    client.invalidateQueries({ queryKey: installationKey });
    client.invalidateQueries({ queryKey: ["ws"] });
  };
  const inst = useInstallation();
  // Managed mode never shows the SMTP form; existing SMTP never shows an installer.
  const managed = inst.data?.readiness.mode === "managed_mta";
  const mailcow = f.provider_type === "mailcow";
  const lastTest = status.data?.last_test as { kind: string; ok: boolean } | null | undefined;
  return (
    <>
      <PageHeader
        title={phrase("Delivery infrastructure")}
        description={phrase(
          "How this installation hands mail to the internet, and whether it is ready. These settings apply to every project.",
        )}
        action={
          managed ? undefined : (
            <Button disabled={testing} onClick={runTest}>
              {testing ? phrase("Testing…") : phrase("Test connection")}
            </Button>
          )
        }
      />
      <div className="mb-4 space-y-4">
        <SetupJourneyPanel />
        <DiscoveryPanel
          onUseCandidate={(c) => {
            setF((x) => ({
              ...x,
              smtp_host: c.host.startsWith("<") ? "" : c.host,
              smtp_port: String(c.port),
              smtp_tls_mode: c.tls_mode,
              provider_type: c.kind === "mailcow" ? "mailcow" : "generic",
            }));
            toast.success(phrase("Settings filled in. Enter the mailbox login, save, then test."));
          }}
        />
        <DeliveryInfrastructure onRecheck={runTest} rechecking={testing} />
        <TransportProfiles />
      </div>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <div className="space-y-4">
          {test && (
            <Section
              title={test.ok ? phrase("Connection test passed") : phrase("Connection test failed")}
            >
              <ol className="space-y-2 text-sm">
                {test.steps.map((st) => (
                  <li key={st.key} className="flex items-start gap-2">
                    <StepIcon st={st} />
                    <span>
                      <span className="font-medium">{phrase(st.title)}</span>
                      <span className="block text-xs text-muted-foreground">{st.detail}</span>
                    </span>
                  </li>
                ))}
                {test.mailcow_api && (
                  <li className="flex items-start gap-2">
                    <StepIcon st={test.mailcow_api} />
                    <span>
                      <span className="font-medium">{phrase("Mailcow API reachable")}</span>
                      <span className="block text-xs text-muted-foreground">
                        {test.mailcow_api.detail}
                      </span>
                    </span>
                  </li>
                )}
              </ol>
            </Section>
          )}
          {env && (
            <p className="rounded-lg border border-primary/40 bg-primary/10 p-3 text-sm">
              {phrase("SMTP is set in the server environment")} ({s(env.username)}@{s(env.host)}:
              {s(env.port)}, {s(env.tls_mode)}).{" "}
              {phrase(
                "The environment takes precedence: the test and the worker use it. The settings below apply when SMTP_HOST is not set.",
              )}
            </p>
          )}
          {!managed && (
            <Section title={phrase("Outbound (SMTP submission)")}>
              <fieldset className="grid gap-3 sm:grid-cols-2">
                <Field
                  label={phrase("Mail server type")}
                  hint={phrase(
                    "Generic works with any SMTP server. Mailcow only adds optional helpers; it is never required to send.",
                  )}
                >
                  <select
                    className={selectClass}
                    value={f.provider_type}
                    onChange={(e) => set("provider_type", e.target.value)}
                  >
                    <option value="generic">{phrase("Generic SMTP")}</option>
                    <option value="mailcow">Mailcow</option>
                  </select>
                </Field>
                <Field
                  label={phrase("SMTP host")}
                  hint={phrase("Your mail server's host name, for example mail.example.com.")}
                >
                  <input
                    className={inputClass}
                    value={f.smtp_host}
                    onChange={(e) => set("smtp_host", e.target.value.trim())}
                  />
                </Field>
                <div className="grid grid-cols-2 gap-3">
                  <Field label={phrase("Port")} error={errors.smtp_port}>
                    <input
                      className={inputClass}
                      inputMode="numeric"
                      value={f.smtp_port}
                      onChange={(e) => set("smtp_port", e.target.value)}
                    />
                  </Field>
                  <Field label={phrase("Encryption")}>
                    <select
                      className={selectClass}
                      value={f.smtp_tls_mode}
                      onChange={(e) => set("smtp_tls_mode", e.target.value)}
                    >
                      <option value="starttls">STARTTLS (587)</option>
                      <option value="implicit">TLS (465)</option>
                    </select>
                  </Field>
                </div>
                <Field
                  label={phrase("Username")}
                  hint={phrase(
                    "A dedicated mailbox, for example njiwa@example.com. Not a personal or admin account.",
                  )}
                >
                  <input
                    className={inputClass}
                    autoComplete="off"
                    value={f.smtp_username}
                    onChange={(e) => set("smtp_username", e.target.value)}
                  />
                </Field>
                <Field
                  label={phrase("Password")}
                  hint={
                    cfg.data?.smtp_password_set
                      ? phrase("A password is stored. Leave empty to keep it.")
                      : phrase("Stored encrypted; never shown again.")
                  }
                >
                  <input
                    className={inputClass}
                    type="password"
                    autoComplete="new-password"
                    value={f.smtp_password}
                    onChange={(e) => set("smtp_password", e.target.value)}
                  />
                </Field>
                <Field
                  label={phrase("Test sender")}
                  error={errors.test_sender}
                  hint={phrase(
                    "An address the login may send as; used by the sender check and test email.",
                  )}
                >
                  <input
                    className={inputClass}
                    type="email"
                    value={f.test_sender}
                    onChange={(e) => set("test_sender", e.target.value)}
                  />
                </Field>
              </fieldset>
            </Section>
          )}
          <Section title={phrase("Inbound")}>
            <fieldset>
              <Field
                label={phrase("Inbound listener address")}
                error={errors.inbound_listener}
                hint={phrase(
                  "Where your mail server relays received mail and bounces to Njiwa-Mail, as host:port on a private network. Used for the reachability check.",
                )}
              >
                <input
                  className={inputClass}
                  value={f.inbound_listener}
                  onChange={(e) => set("inbound_listener", e.target.value.trim())}
                  placeholder="njiwa-inbound:2525"
                />
              </Field>
            </fieldset>
          </Section>
          {!managed && !mailcow && inst.data?.active_profile?.mode === "existing_smtp" && (
            <SendingFactsForm
              key={inst.data.active_profile.id}
              profileId={inst.data.active_profile.id}
              facts={inst.data.active_profile.sending_facts}
            />
          )}
          {!managed && mailcow && (
            <Section title={phrase("Mailcow API (optional)")}>
              <p className="mb-3 text-xs text-muted-foreground">
                {phrase(
                  "With the API, the Domains page can add domains, create DKIM keys and add the inbound transport for you. Without it, the Domains page shows the manual steps. Njiwa-Mail never changes global Mailcow settings.",
                )}
              </p>
              <fieldset className="grid gap-3 sm:grid-cols-2">
                <Field label={phrase("API URL")} error={errors.mailcow_api_url}>
                  <input
                    className={inputClass}
                    value={f.mailcow_api_url}
                    onChange={(e) => set("mailcow_api_url", e.target.value.trim())}
                    placeholder="https://mail.example.com"
                  />
                </Field>
                <Field
                  label={phrase("API key")}
                  hint={
                    cfg.data?.mailcow_api_key_set
                      ? phrase("A key is stored. Leave empty to keep it.")
                      : phrase(
                          "Read-write key from Mailcow → System → Configuration → Access → API.",
                        )
                  }
                >
                  <input
                    className={inputClass}
                    type="password"
                    autoComplete="new-password"
                    value={f.mailcow_api_key}
                    onChange={(e) => set("mailcow_api_key", e.target.value)}
                  />
                </Field>
              </fieldset>
              <a
                className="mt-3 inline-block text-xs text-primary"
                href="https://github.com/njiwa-tz/mail/blob/main/docs/MAILCOW_SETUP.md"
                target="_blank"
                rel="noreferrer"
              >
                {phrase("Mailcow operator steps (docs/MAILCOW_SETUP.md)")}
              </a>
            </Section>
          )}
          <Button onClick={save} disabled={Object.keys(errors).length > 0}>
            {phrase("Save settings")}
          </Button>
        </div>
        <div className="space-y-4">
          <Section title={phrase("Status")}>
            <DataState q={status}>
              {(st) => (
                <dl className="space-y-2 text-sm">
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Outbound settings")}</dt>
                    <dd>{s(st.outbound_source)}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Outbound connection")}</dt>
                    <dd>
                      {lastTest ? (
                        <StateBadge
                          state={lastTest.ok ? "pass" : "fail"}
                          label={
                            lastTest.ok ? phrase("last test passed") : phrase("last test failed")
                          }
                        />
                      ) : (
                        phrase("not tested")
                      )}{" "}
                      <Time value={st.last_test_at} relative />
                    </dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Inbound listener reachable")}</dt>
                    <dd>
                      {st.inbound_reachable === null ? (
                        phrase("address not set")
                      ) : st.inbound_reachable ? (
                        <StateBadge state="pass" label={phrase("yes")} />
                      ) : (
                        <StateBadge state="fail" label={phrase("no")} />
                      )}
                    </dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Last inbound message")}</dt>
                    <dd>
                      <Time value={st.last_inbound_at} relative />
                    </dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Last bounce report")}</dt>
                    <dd>
                      <Time value={st.last_bounce_at} relative />
                    </dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Queue depth")}</dt>
                    <dd>{s(st.queue_depth)}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Waiting to retry")}</dt>
                    <dd>{s(st.retry_wait)}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Unknown outcome")}</dt>
                    <dd>{s(st.unknown)}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Inbound domain")}</dt>
                    <dd>{s(st.inbound_domain) || phrase("not set")}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>{phrase("Bounce address")}</dt>
                    <dd>{s(st.bounce_address) || phrase("not set")}</dd>
                  </div>
                  <p className="pt-2 text-xs text-muted-foreground">
                    {phrase(
                      "There is no circuit breaker: a temporary failure is retried per message with backoff; a message whose outcome is unknown is never retried automatically.",
                    )}
                  </p>
                </dl>
              )}
            </DataState>
          </Section>
          {!managed && (
            <Section title={phrase("Send test email")}>
              <p className="mb-2 text-xs text-muted-foreground">
                {phrase(
                  "Send through the queue to test the real path: the message is accepted, queued and sent by the worker, and the result is recorded for these settings. Send directly for a quick check of the connection only.",
                )}
              </p>
              <div className="flex gap-2">
                <input
                  aria-label={phrase("Send test to")}
                  className={inputClass}
                  type="email"
                  value={sendTo}
                  onChange={(e) => setSendTo(e.target.value)}
                  placeholder="you@gmail.com"
                />
                <Button
                  disabled={!emailOk(sendTo)}
                  onClick={async () => {
                    const r = await run(
                      () => admin.send<Row>("/mail-server/test-send", "POST", { to: sendTo }),
                      "",
                    );
                    if (r) setSendResult(r);
                    client.invalidateQueries({ queryKey: ["mail-server"] });
                    client.invalidateQueries({ queryKey: installationKey });
                  }}
                >
                  {phrase("Send directly")}
                </Button>
                <Button
                  variant="outline"
                  disabled={!emailOk(sendTo) || !project}
                  onClick={async () => {
                    const r = await run(
                      () =>
                        admin.send<Row>(`/w/${project}/live/transport-test`, "POST", {
                          to: sendTo,
                        }),
                      phrase("Test queued. The result appears above once the worker has sent it."),
                    );
                    if (r) setQueued(s(r.id));
                    client.invalidateQueries({ queryKey: installationKey });
                  }}
                >
                  {phrase("Send through the queue")}
                </Button>
              </div>
              {queued && (
                <p role="status" className="mt-2 text-sm">
                  {phrase("Queued from project")} {project}:{" "}
                  <Link className="text-primary underline" to={wsTo(`messages/${queued}`)}>
                    {phrase("view the message")}
                  </Link>
                </p>
              )}
              {sendResult && (
                <p role="status" className="mt-2 text-sm">
                  <StateBadge state={s(sendResult.state)} /> {s(sendResult.reason)}{" "}
                  {s(sendResult.reply)}
                </p>
              )}
            </Section>
          )}
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------- analytics, alerts, system, audit

export function Analytics() {
  const phrase = usePhrase();
  const { api, env, project } = useWorkspace();
  const key = useWsKey();
  const q = useQuery({
    queryKey: key("analytics"),
    queryFn: () => api.get<{ items: Row[] }>("/analytics"),
  });
  return (
    <>
      <PageHeader
        title={phrase("Analytics")}
        description={`${phrase("Daily totals for")} ${project} (${env}).`}
      />
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={<Empty title={phrase("No activity recorded yet")} />}
      >
        {(d) => {
          const cols = Object.keys(d.items[0] ?? {});
          return (
            <Table
              rows={d.items}
              rowKey={(r) => JSON.stringify(r)}
              columns={cols.map((c) => ({
                key: c,
                label: phrase(c.replace(/_/g, " ")),
                render: (r) => (/_at$/.test(c) ? <Time value={r[c]} /> : s(r[c])),
              }))}
            />
          );
        }}
      </DataState>
    </>
  );
}

export function Alerts() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [open, setOpen] = useParam("open", "1");
  const [kind, setKind] = useState("email");
  const [target, setTarget] = useState("");
  const offset = useOffset();
  const q = useQuery({
    queryKey: ["alerts", open, offset],
    queryFn: () => admin.get<Page>(`/alerts?limit=50&offset=${offset}${open ? "&open=true" : ""}`),
  });
  const channels = useQuery({
    queryKey: ["alert-channels"],
    queryFn: () => admin.get<{ items: Row[] }>("/alert-channels"),
  });
  return (
    <>
      <PageHeader
        title={phrase("Alerts")}
        description={phrase(
          "Problems that need an operator: policy bounces, paused announcements, disabled webhooks, failing checks.",
        )}
      />
      <label className="mb-3 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={!!open}
          onChange={(e) => setOpen(e.target.checked ? "1" : "")}
        />{" "}
        {phrase("Only open alerts")}
      </label>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty title={phrase("No open alerts")} />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                { key: "kind", label: phrase("Kind") },
                { key: "summary", label: phrase("Summary"), className: "whitespace-normal" },
                {
                  key: "created_at",
                  label: phrase("Raised"),
                  render: (r) => <Time value={r.created_at} />,
                },
                {
                  key: "ack",
                  label: phrase("Acknowledged"),
                  render: (r) =>
                    r.acknowledged_at ? (
                      <>
                        {s(r.acknowledged_by)} <Time value={r.acknowledged_at} />
                      </>
                    ) : (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={async () => {
                          await run(
                            () => admin.send(`/alerts/${s(r.id)}/ack`, "POST"),
                            phrase("Acknowledged."),
                          );
                          client.invalidateQueries({ queryKey: ["alerts"] });
                        }}
                      >
                        {phrase("Acknowledge")}
                      </Button>
                    ),
                },
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
      <Section className="mt-6" title={phrase("Where alerts are sent")}>
        <form
          className="mb-3 flex flex-wrap items-end gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            if (kind === "email" ? !emailOk(target) : !target.startsWith("https://")) return;
            if (
              await run(
                () => admin.send("/alert-channels", "POST", { kind, target }),
                phrase("Channel added."),
              )
            ) {
              setTarget("");
              client.invalidateQueries({ queryKey: ["alert-channels"] });
            }
          }}
        >
          <Field label={phrase("Kind")}>
            <select className={selectClass} value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="email">email</option>
              <option value="webhook">webhook</option>
            </select>
          </Field>
          <Field label={kind === "email" ? phrase("Email") : "HTTPS URL"} className="flex-1">
            <input
              className={inputClass}
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            />
          </Field>
          <Button type="submit">{phrase("Add")}</Button>
        </form>
        <DataState
          q={channels}
          isEmpty={(d) => d.items.length === 0}
          empty={
            <p className="text-sm text-muted-foreground">
              {phrase("No channels: alerts are only shown here.")}
            </p>
          }
        >
          {(d) => (
            <ul className="text-sm">
              {d.items.map((c) => (
                <li key={s(c.id)}>
                  {s(c.kind)}: {s(c.target)} {c.enabled ? "" : `(${phrase("disabled")})`}
                </li>
              ))}
            </ul>
          )}
        </DataState>
      </Section>
    </>
  );
}

type Overview = {
  counts: Record<string, number>;
  sent_today: number;
  queue_depth: number;
  webhook_backlog: number;
  open_alerts: number;
  smtp_configured: boolean;
  inbound_domain: string;
  bounce_address: string;
  dns_ok: boolean | null;
  workers: { role: string; instance: string; healthy: boolean; last_seen: string }[];
};

export function System() {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const q = useQuery({
    queryKey: ["overview"],
    queryFn: () => admin.get<Overview>("/overview"),
    refetchInterval: 15_000,
  });
  return (
    <>
      <PageHeader
        title={phrase("System")}
        description={phrase("The whole service, across all projects.")}
      />
      <DataState q={q}>
        {(d) => (
          <div className="grid gap-4 lg:grid-cols-2">
            <Section title={phrase("Processes")}>
              {d.workers.length === 0 ? (
                <p className="text-sm text-destructive">
                  {phrase("No worker, scheduler or inbound process has reported in the last hour.")}
                </p>
              ) : (
                <ul className="space-y-1 text-sm">
                  {d.workers.map((w) => (
                    <li key={w.role + w.instance} className="flex items-center gap-2">
                      <StateBadge
                        state={w.healthy ? "pass" : "fail"}
                        label={w.healthy ? phrase("healthy") : phrase("silent")}
                      />{" "}
                      {w.role} <span className="text-xs text-muted-foreground">{w.instance}</span>
                      <span className="ml-auto text-xs">
                        <Time value={w.last_seen} relative />
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </Section>
            <Section title={phrase("Service")}>
              <dl className="space-y-2 text-sm">
                {(
                  [
                    ["Submitted today (all projects)", d.sent_today],
                    ["Queue depth", d.queue_depth],
                    ["Webhook backlog", d.webhook_backlog],
                    ["Open alerts", d.open_alerts],
                    [
                      "SMTP from environment",
                      d.smtp_configured ? phrase("yes") : phrase("no — see Mail server"),
                    ],
                    ["Inbound domain", d.inbound_domain || phrase("not set")],
                    ["Bounce address", d.bounce_address || phrase("not set")],
                  ] as [string, unknown][]
                ).map(([k, v]) => (
                  <div key={k} className="flex justify-between gap-2">
                    <dt>{phrase(k)}</dt>
                    <dd>{s(v)}</dd>
                  </div>
                ))}
              </dl>
              <Link className="mt-3 inline-block text-sm text-primary" to={to("mail-server")}>
                {phrase("Mail server →")}
              </Link>
            </Section>
          </div>
        )}
      </DataState>
    </>
  );
}

export function Audit() {
  const phrase = usePhrase();
  const [actor, setActor] = useParam("actor");
  const [action, setAction] = useParam("action");
  const offset = useOffset();
  const q = useQuery({
    queryKey: ["audit", actor, action, offset],
    queryFn: () =>
      admin.get<Page>(
        `/audit?limit=50&offset=${offset}&actor=${encodeURIComponent(actor)}&action=${encodeURIComponent(action)}`,
      ),
  });
  return (
    <>
      <PageHeader
        title={phrase("Audit log")}
        description={phrase("Every change made by a person or an API key.")}
      />
      <div className="mb-3 grid gap-3 sm:grid-cols-2">
        <Field label={phrase("Actor")}>
          <input
            className={inputClass}
            type="search"
            defaultValue={actor}
            onBlur={(e) => setActor(e.target.value.trim())}
          />
        </Field>
        <Field label={phrase("Action")}>
          <input
            className={inputClass}
            type="search"
            defaultValue={action}
            onBlur={(e) => setAction(e.target.value.trim())}
            placeholder="domain.create"
          />
        </Field>
      </div>
      <DataState q={q}>
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                { key: "at", label: phrase("When"), render: (r) => <Time value={r.at} /> },
                { key: "actor", label: phrase("Actor") },
                { key: "action", label: phrase("Action") },
                {
                  key: "target",
                  label: phrase("Target"),
                  render: (r) => `${s(r.target_type)} ${s(r.target_id)}`,
                },
                {
                  key: "details",
                  label: phrase("Details"),
                  render: (r) => (
                    <code className="text-xs">{r.details ? JSON.stringify(r.details) : ""}</code>
                  ),
                },
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
    </>
  );
}

// ---------------------------------------------------------------- privacy (scoped)

export function Privacy() {
  const phrase = usePhrase();
  const { api, project, env } = useWorkspace();
  const [email, setEmail] = useState("");
  return (
    <>
      <PageHeader
        title={phrase("Recipient privacy")}
        description={`${phrase("Export or delete one recipient's stored data in")} ${project} (${env}).`}
      />
      <Section>
        <div className="flex flex-wrap items-end gap-3">
          <Field label={phrase("Recipient email")} className="flex-1">
            <input
              className={inputClass}
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Field>
          <Button
            variant="outline"
            disabled={!emailOk(email)}
            onClick={async () => {
              const r = await run(
                () => api.send<unknown>("/privacy/export", "POST", { email }),
                phrase("Export downloaded."),
              );
              if (!r) return;
              const url = URL.createObjectURL(
                new Blob([JSON.stringify(r, null, 2)], { type: "application/json" }),
              );
              const a = document.createElement("a");
              a.href = url;
              a.download = "njiwa-mail-recipient-export.json";
              a.click();
              URL.revokeObjectURL(url);
            }}
          >
            {phrase("Export")}
          </Button>
          <Button
            variant="outline"
            disabled={!emailOk(email)}
            onClick={async () => {
              // The CSV comes from the server (formula-safe); a POST, so fetch it here.
              try {
                const res = await fetch(`/admin/v1${api.base}/privacy/export?format=csv`, {
                  method: "POST",
                  credentials: "same-origin",
                  headers: { "Content-Type": "application/json", "X-CSRF-Token": getCsrf() },
                  body: JSON.stringify({ email }),
                });
                if (!res.ok) throw new Error(phrase("Request failed"));
                const url = URL.createObjectURL(await res.blob());
                const a = document.createElement("a");
                a.href = url;
                a.download = "njiwa-mail-recipient-export.csv";
                a.click();
                URL.revokeObjectURL(url);
              } catch (e) {
                toast.error(e instanceof Error ? e.message : phrase("Request failed"));
              }
            }}
          >
            {phrase("Export CSV")}
          </Button>
          <ConfirmButton
            size="default"
            disabled={!emailOk(email)}
            title={`${phrase("Delete all stored data for")} ${email}?`}
            description={phrase(
              "Message metadata, delivery evidence, bounces, suppressions, captures and inbound mail (with its attachment files) for this recipient in this project and environment are removed. This cannot be undone.",
            )}
            confirm="Delete data"
            onConfirm={() =>
              run(
                () => api.send("/privacy/delete", "POST", { email }),
                phrase("Recipient data deleted."),
              )
            }
          >
            {phrase("Delete")}
          </ConfirmButton>
        </div>
      </Section>
    </>
  );
}
