import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, LogIn, MinusCircle, Plus, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { Sessions } from "@/features/mail/Sessions";
import { Platform } from "./Platform";
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
import { ExistingMailServerSetup } from "@/features/installation/ExistingMailServerSetup";
import { TransportProfiles } from "@/features/installation/Profiles";
import { useSendingEnvironment, useWorkspace, useWsKey, type ProjectRow } from "../context";
import { SendingMode, useSendingModeParam } from "../SendingMode";
import { NewProjectDialog } from "../Layout";
import {
  ConfirmButton,
  CopyButton,
  DataState,
  Empty,
  emailOk,
  errorMessage,
  Field,
  inputClass,
  textareaClass,
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
  const [passwordFor, setPasswordFor] = useState<Row | null>(null);
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
                      <Button size="sm" variant="outline" onClick={() => setPasswordFor(r)}>
                        {phrase("Change password")}
                      </Button>
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
      <PasswordDialog user={passwordFor} onClose={() => setPasswordFor(null)} onSaved={refresh} />
    </>
  );
}

function PasswordDialog({
  user,
  onClose,
  onSaved,
}: {
  user: Row | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const phrase = usePhrase();
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    setNext("");
    setAgain("");
    setError("");
  }, [user]);
  const save = async () => {
    if (!user) return;
    if (next.length < 12) {
      setError(phrase("The temporary password needs at least 12 characters."));
      return;
    }
    if (next !== again) {
      setError(phrase("The two new passwords are not the same."));
      return;
    }
    setError("");
    if (
      await run(
        () => admin.send(`/users/${s(user.id)}`, "PATCH", { password: next }),
        phrase("Password changed. They were signed out."),
      )
    ) {
      onSaved();
      onClose();
    }
  };
  return (
    <Dialog open={!!user} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogTitle>{phrase("Change password")}</DialogTitle>
        <DialogDescription>
          {s(user?.email)}
          {". "}
          {phrase(
            "Set a new password. They are signed out and must use it the next time they sign in.",
          )}
        </DialogDescription>
        <div className="grid gap-3">
          <Field label={phrase("New password")}>
            <input
              className={inputClass}
              type="password"
              autoComplete="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          </Field>
          <Field label={phrase("Repeat the new password")}>
            <input
              className={inputClass}
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
            />
          </Field>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button onClick={save} disabled={!next || !again}>
              {phrase("Save")}
            </Button>
            <Button type="button" variant="outline" onClick={onClose}>
              {phrase("Cancel")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
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
      {!managed && <ExistingMailServerSetup />}
      <details className="mt-4 rounded-lg border p-4" open={managed || undefined}>
        <summary className="cursor-pointer font-medium">
          {phrase("Advanced Settings and installation status")}
        </summary>
        <div className="mb-4 mt-4 space-y-4">
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
              toast.success(
                phrase("Settings filled in. Enter the mailbox login, save, then test."),
              );
            }}
          />
          <div id="delivery-infrastructure" className="scroll-mt-24">
            <DeliveryInfrastructure
              onRecheck={runTest}
              rechecking={testing}
              showModeChoice={false}
            />
          </div>
          <TransportProfiles />
        </div>
        <div
          id="advanced-delivery-connection"
          className="scroll-mt-24 grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]"
        >
          <div className="space-y-4">
            {test && (
              <Section
                title={
                  test.ok ? phrase("Connection test passed") : phrase("Connection test failed")
                }
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
                        ) : !st.inbound_reachable && !st.inbound_domain && !st.bounce_address ? (
                          // Inbound mail and bounces are not turned on, so nothing is
                          // expected to listen there: not a failure.
                          <StateBadge state="cancelled" label={phrase("not in use")} />
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
                        phrase(
                          "Test queued. The result appears above once the worker has sent it.",
                        ),
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
      </details>
    </>
  );
}

// ---------------------------------------------------------------- analytics, alerts, system, audit

export function Analytics() {
  const phrase = usePhrase();
  const { project } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const { api, key } = useSendingEnvironment(env);
  const [days, setDays] = useParam("days", "30");
  const end = new Date();
  end.setUTCHours(0, 0, 0, 0);
  end.setUTCDate(end.getUTCDate() + 1);
  const start = new Date(end.getTime() - Number(days) * 86400000);
  const range = `from=${start.toISOString().slice(0, 10)}&to=${end.toISOString().slice(0, 10)}`;
  const q = useQuery({
    queryKey: key("report", range),
    queryFn: () => api.get<Row>(`/report?${range}`),
  });
  const csv = (table: string) => `/admin/v1${api.base}/report?format=csv&table=${table}&${range}`;
  const metrics: [string, string][] = [
    ["messages", "Messages"],
    ["submitted", "Accepted by the mail server"],
    ["remote_accepted", "Accepted by the recipient's server"],
    ["failed", "Failed"],
    ["hard_bounces", "Hard bounces"],
    ["complaints", "Complaints"],
  ];
  const secs = (v: unknown) => {
    const n = Number(v);
    if (v === null || v === undefined || Number.isNaN(n)) return "—";
    return n < 60 ? `${n.toFixed(1)} s` : `${(n / 60).toFixed(1)} min`;
  };
  const upIsGood = new Set(["messages", "submitted", "remote_accepted"]);
  const changeClass = (key: string, value: unknown) => {
    if (value === null || value === undefined) return "od-delta od-delta-neutral";
    const n = Number(value);
    if (!n) return "od-delta od-delta-neutral";
    const better = upIsGood.has(key) ? n > 0 : n < 0;
    return better ? "od-delta od-delta-good" : "od-delta od-delta-bad";
  };
  const shownEnd = new Date(end.getTime() - 86400000);
  const periodLabel = `${start.toISOString().slice(0, 10)} – ${shownEnd.toISOString().slice(0, 10)}`;
  return (
    <>
      <PageHeader
        title={phrase("Analytics")}
        description={`${project}: ${phrase("this period compared with the one before it.")}`}
      />
      <section className="od-card mb-4">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="od-kpi-label">{phrase("Sending mode")}</p>
            <div className="od-chips mt-2" role="group" aria-label={phrase("Sending mode")}>
              {(
                [
                  ["live", phrase("Live — send to real recipients")],
                  ["sandbox", phrase("Sandbox — capture only")],
                ] as const
              ).map(([mode, label]) => (
                <button
                  key={mode}
                  type="button"
                  aria-checked={env === mode}
                  className="od-chip"
                  onClick={() => setEnv(mode)}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
          <div>
            <p className="od-kpi-label">{phrase("Period")}</p>
            <div className="od-chips mt-2" role="group" aria-label={phrase("Period")}>
              {(
                [
                  ["7", phrase("Last 7 days")],
                  ["30", phrase("Last 30 days")],
                  ["90", phrase("Last 90 days")],
                ] as const
              ).map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  aria-checked={days === value}
                  className="od-chip"
                  onClick={() => setDays(value)}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
          <p className="text-sm text-muted-foreground" role="status">
            {periodLabel}
            {q.isFetching ? ` · ${phrase("Loading…")}` : ""}
          </p>
        </div>
      </section>
      <DataState q={q}>
        {(r) => {
          const tot = (r.totals as Row) ?? {};
          const ch = (r.change_percent as Row) ?? {};
          const tm = (r.timing_seconds as Row) ?? {};
          const providers = (r.providers as Row[]) ?? [];
          const daily = (r.daily as Row[]) ?? [];
          const timings = [
            {
              k: "submit",
              label: phrase("Until the mail server accepted it"),
              p50: tm.submit_p50,
              p90: tm.submit_p90,
              p99: tm.submit_p99,
              n: tm.submit_samples,
            },
            {
              k: "remote",
              label: phrase("Until the recipient's server accepted it"),
              p50: tm.remote_p50,
              p90: tm.remote_p90,
              p99: tm.remote_p99,
              n: tm.remote_samples,
            },
          ];
          return (
            <>
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
                {metrics.map(([k, label]) => {
                  const c = ch[k];
                  return (
                    <article key={k} className="od-card">
                      <p className="od-kpi-label">{phrase(label)}</p>
                      <p className="od-kpi-value">{s(tot[k]) || "0"}</p>
                      <p className={`mt-2 text-xs font-medium ${changeClass(k, c)}`}>
                        {c === null || c === undefined
                          ? phrase("no earlier data")
                          : `${Number(c) > 0 ? "+" : ""}${Number(c).toFixed(0)}% ${phrase("vs previous period")}`}
                      </p>
                    </article>
                  );
                })}
              </div>
              <section className="mt-4">
                <h2 className="mb-3 text-sm font-semibold">{phrase("How long it took")}</h2>
                <div className="grid gap-3 lg:grid-cols-2">
                  {timings.map((t) => (
                    <article key={t.k} className="od-card">
                      <h3 className="od-card-title">{t.label}</h3>
                      <dl className="mt-4 grid grid-cols-3 gap-3">
                        {(
                          [
                            [phrase("Median"), t.p50],
                            ["p90", t.p90],
                            ["p99", t.p99],
                          ] as const
                        ).map(([label, value]) => (
                          <div key={label}>
                            <dt className="text-xs text-muted-foreground">{label}</dt>
                            <dd className="mt-1 text-lg font-semibold">{secs(value)}</dd>
                          </div>
                        ))}
                      </dl>
                      <p className="mt-3 text-xs text-muted-foreground">
                        {s(t.n) || "0"} {phrase("Messages measured")}
                      </p>
                    </article>
                  ))}
                </div>
              </section>
              <Section
                className="mt-4"
                title={phrase("By mailbox provider")}
                action={
                  <a className="text-sm text-primary" href={csv("providers")}>
                    {phrase("Download CSV")}
                  </a>
                }
              >
                {providers.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    {phrase("No activity in this period")}
                  </p>
                ) : (
                  <Table
                    rows={providers}
                    rowKey={(x) => s(x.provider)}
                    columns={[
                      { key: "provider", label: phrase("Provider") },
                      { key: "messages", label: phrase("Messages") },
                      { key: "submitted", label: phrase("Submitted") },
                      { key: "remote_accepted", label: phrase("Remote accepted") },
                      { key: "hard_bounces", label: phrase("Hard bounces") },
                      { key: "complaints", label: phrase("Complaints") },
                    ]}
                  />
                )}
              </Section>
              <Section
                className="mt-4"
                title={phrase("Per day")}
                action={
                  <span className="flex gap-3 text-sm">
                    <a className="text-primary" href={csv("daily")}>
                      {phrase("Download CSV")}
                    </a>
                    <a className="text-primary" href={csv("totals")}>
                      {phrase("Totals CSV")}
                    </a>
                  </span>
                }
              >
                {daily.length === 0 ? (
                  <Empty title={phrase("No activity in this period")} />
                ) : (
                  <Table
                    rows={daily}
                    rowKey={(x) => s(x.day)}
                    columns={[
                      { key: "day", label: phrase("Day") },
                      { key: "messages", label: phrase("Messages") },
                      { key: "submitted", label: phrase("Submitted") },
                      { key: "failed", label: phrase("Failed") },
                      { key: "hard_bounces", label: phrase("Hard bounces") },
                      { key: "soft_bounces", label: phrase("Soft bounces") },
                    ]}
                  />
                )}
              </Section>
              {((r.notes as string[]) ?? []).length > 0 && (
                <ul className="mt-4 space-y-1 rounded-xl border bg-card px-4 py-3 text-xs text-muted-foreground">
                  {((r.notes as string[]) ?? []).map((note) => (
                    <li key={note}>{note}</li>
                  ))}
                </ul>
              )}
            </>
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
  const { user } = useAuth();
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
        description={phrase(
          "The whole service, across all projects, with live health, scheduled jobs and updates.",
        )}
      />
      <DataState q={q}>
        {(d) => (
          <div className="grid gap-4 lg:grid-cols-2">
            <section className="rounded-2xl border bg-card p-5">
              <h2 className="font-semibold">{phrase("Processes")}</h2>
              {d.workers.length === 0 ? (
                <p className="mt-3 text-sm text-destructive">
                  {phrase("No worker, scheduler or inbound process has reported in the last hour.")}
                </p>
              ) : (
                <ul className="mt-4 space-y-2">
                  {d.workers.map((w) => (
                    <li
                      key={w.role + w.instance}
                      className="flex items-center gap-3 rounded-xl border bg-background px-3 py-3"
                    >
                      <StateBadge
                        state={w.healthy ? "pass" : "fail"}
                        label={w.healthy ? phrase("healthy") : phrase("silent")}
                      />
                      <span className="min-w-0">
                        <span className="block text-sm font-medium">{w.role}</span>
                        <span className="block truncate text-xs text-muted-foreground">
                          {w.instance}
                        </span>
                      </span>
                      <span className="ml-auto text-xs text-muted-foreground">
                        <Time value={w.last_seen} relative />
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <section className="rounded-2xl border bg-card p-5">
              <h2 className="font-semibold">{phrase("Service")}</h2>
              <div className="mt-4 grid grid-cols-2 gap-3">
                {(
                  [
                    ["Submitted today (all projects)", d.sent_today],
                    ["Queue depth", d.queue_depth],
                    ["Webhook backlog", d.webhook_backlog],
                    ["Open alerts", d.open_alerts],
                  ] as [string, unknown][]
                ).map(([label, value]) => (
                  <div key={label} className="rounded-xl border bg-background p-3">
                    <p className="text-xs text-muted-foreground">{phrase(label)}</p>
                    <p className="mt-1 text-2xl font-semibold">{s(value)}</p>
                  </div>
                ))}
              </div>
              <dl className="mt-4 space-y-2 text-sm">
                {(
                  [
                    [
                      "SMTP from environment",
                      d.smtp_configured ? phrase("yes") : phrase("no — see Mail server"),
                    ],
                    ["Inbound domain", d.inbound_domain || phrase("not set")],
                    ["Bounce address", d.bounce_address || phrase("not set")],
                  ] as [string, unknown][]
                ).map(([label, value]) => (
                  <div key={label} className="flex justify-between gap-3 border-t pt-2">
                    <dt className="text-muted-foreground">{phrase(label)}</dt>
                    <dd className="text-right">{s(value)}</dd>
                  </div>
                ))}
              </dl>
              <Link className="mt-4 inline-block text-sm text-primary" to={to("mail-server")}>
                {phrase("Mail server →")}
              </Link>
            </section>
          </div>
        )}
      </DataState>
      {user?.installation_operator && <Platform embedded />}
    </>
  );
}

export function Audit() {
  const phrase = usePhrase();
  const [query, setQuery] = useParam("q");
  const [draft, setDraft] = useState(query);
  const offset = useOffset();
  useEffect(() => setDraft(query), [query]);
  const q = useQuery({
    queryKey: ["audit", query, offset],
    queryFn: () =>
      admin.get<Page>(`/audit?limit=50&offset=${offset}&q=${encodeURIComponent(query)}`),
  });
  return (
    <>
      <PageHeader
        title={phrase("Audit log")}
        description={phrase("Every change made by a person or an API key.")}
      />
      <form
        className="relative mb-4"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(draft.trim());
        }}
      >
        <div className="od-card flex-row flex-wrap items-end">
          <Field label={phrase("Search the log")} className="min-w-64 flex-1">
            <input
              className={inputClass}
              type="search"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={phrase("Actor, action, or target")}
            />
          </Field>
          <Button type="submit">{phrase("Search")}</Button>
          {query && (
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setDraft("");
                setQuery("");
              }}
            >
              {phrase("Clear")}
            </Button>
          )}
          {query && (
            <p className="w-full text-xs text-muted-foreground">
              {phrase("Showing events that match")} <strong>{query}</strong>
            </p>
          )}
        </div>
      </form>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card px-6 py-12 text-center">
            <p className="font-semibold">{phrase("No events match this search.")}</p>
          </div>
        }
      >
        {(d) => (
          <>
            <ol className="space-y-2">
              {d.items.map((r) => (
                <li key={s(r.id)} className="rounded-xl border bg-card p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <code className="text-sm font-semibold">{s(r.action)}</code>
                      <p className="mt-1 text-sm text-muted-foreground">{s(r.actor) || "—"}</p>
                    </div>
                    <Time value={r.at} />
                  </div>
                  <p className="mt-3 text-sm">
                    <span className="text-muted-foreground">{phrase("Target")}: </span>
                    {s(r.target_type)} {s(r.target_id)}
                  </p>
                  {r.details != null &&
                    typeof r.details === "object" &&
                    Object.keys(r.details as object).length > 0 && (
                      <pre className="mt-2 overflow-x-auto rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
                        {JSON.stringify(r.details)}
                      </pre>
                    )}
                </li>
              ))}
            </ol>
            <Pager page={d} />
          </>
        )}
      </DataState>
      <Sessions />
    </>
  );
}

// ---------------------------------------------------------------- privacy (scoped)

type BulkResult = { email: string; status: string; found?: Record<string, number>; error?: string };
type BulkReply = { dry_run: boolean; results: BulkResult[]; summary: Record<string, number> };

function BulkDelete() {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const [text, setText] = useState("");
  const [reply, setReply] = useState<BulkReply | null>(null);
  const [error, setError] = useState("");
  const emails = text
    .split(/[\s,;]+/)
    .map((x) => x.trim())
    .filter(Boolean);
  const call = async (dry: boolean) => {
    setError("");
    try {
      setReply(await api.send<BulkReply>("/privacy/delete-bulk", "POST", { emails, dry_run: dry }));
    } catch (e) {
      setReply(null);
      setError(errorMessage(e));
    }
  };
  return (
    <Section title={phrase("Delete many recipients")} className="mt-4">
      <p className="mb-3 text-sm text-muted-foreground">
        {phrase(
          "Paste up to 500 addresses, separated by spaces, commas or new lines. Check first: nothing is deleted until you confirm.",
        )}
      </p>
      <textarea
        className={textareaClass}
        rows={5}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setReply(null);
        }}
      />
      {error && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button variant="outline" disabled={emails.length === 0} onClick={() => call(true)}>
          {phrase("Check what would be deleted")}
        </Button>
        <ConfirmButton
          size="default"
          disabled={emails.length === 0 || !reply?.dry_run}
          title={`${phrase("Delete all stored data for")} ${emails.length}`}
          description={phrase(
            "Messages, evidence, bounces, suppressions, inbound mail, contacts, form submissions and chats for these addresses in this project and environment are removed. This cannot be undone.",
          )}
          confirm="Delete data"
          onConfirm={() => call(false)}
        >
          {phrase("Delete")}
        </ConfirmButton>
        {emails.length > 0 && (
          <span className="text-sm text-muted-foreground">
            {emails.length} {phrase("addresses")}
          </span>
        )}
      </div>
      {reply && (
        <div className="mt-3 text-sm" role="status">
          <p className="font-medium">
            {phrase(reply.dry_run ? "Would be deleted" : "Result")}:{" "}
            {Object.entries(reply.summary)
              .map(([k, v]) => `${phrase(k)} ${v}`)
              .join(" · ")}
          </p>
          <ul className="mt-2 max-h-64 overflow-auto rounded border">
            {reply.results.map((r, i) => (
              <li key={i} className="flex justify-between gap-3 border-b p-2 last:border-b-0">
                <span>{r.email}</span>
                <span className="text-muted-foreground">
                  {phrase(r.status)}
                  {r.found && Object.keys(r.found).length > 0
                    ? ` (${Object.entries(r.found)
                        .map(([k, v]) => `${k} ${v}`)
                        .join(", ")})`
                    : ""}
                  {r.error ? ` ${r.error}` : ""}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Section>
  );
}

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
      <BulkDelete />
    </>
  );
}

export function Announcements() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [f, setF] = useState({ title: "", body: "", link: "" });
  const offset = useOffset();
  const q = useQuery({
    queryKey: ["announcements", offset],
    queryFn: () => admin.get<Page>(`/announcements?limit=50&offset=${offset}`),
  });
  const post = async () => {
    const r = await run(
      () => admin.send("/announcements", "POST", f),
      phrase("Announcement sent."),
    );
    if (r) setF({ title: "", body: "", link: "" });
    client.invalidateQueries({ queryKey: ["announcements"] });
    client.invalidateQueries({ queryKey: ["notifications"] });
  };
  return (
    <>
      <PageHeader
        title={phrase("Announcements")}
        description={phrase(
          "A message in every user's notifications. You can retract it afterwards.",
        )}
      />
      <Section title={phrase("New announcement")}>
        <div className="grid gap-3">
          <Field label={phrase("Title")}>
            <input
              className={inputClass}
              value={f.title}
              onChange={(e) => setF({ ...f, title: e.target.value })}
            />
          </Field>
          <Field label={phrase("Message (optional)")}>
            <textarea
              className={textareaClass}
              rows={3}
              value={f.body}
              onChange={(e) => setF({ ...f, body: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Link (optional)")}
            hint={phrase("A page here, such as /p/project/domains, or an https:// address.")}
          >
            <input
              className={inputClass}
              value={f.link}
              onChange={(e) => setF({ ...f, link: e.target.value })}
            />
          </Field>
          <div>
            <Button onClick={post} disabled={!f.title.trim()}>
              {phrase("Send to everyone")}
            </Button>
          </div>
        </div>
      </Section>
      <DataState q={q} isEmpty={(d) => d.items.length === 0} empty={null}>
        {(d) => (
          <Section className="mt-4" title={phrase("Sent")}>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                { key: "title", label: phrase("Title") },
                {
                  key: "created_at",
                  label: phrase("Sent"),
                  render: (r) => <Time value={r.created_at} relative />,
                },
                { key: "read_by", label: phrase("Read by") },
                {
                  key: "x",
                  label: <span className="sr-only">{phrase("Actions")}</span>,
                  render: (r) =>
                    r.retracted_at ? (
                      <StateBadge state="cancelled" label={phrase("retracted")} />
                    ) : (
                      <ConfirmButton
                        title={phrase("Retract this announcement?")}
                        description={phrase("It disappears from every user's notifications.")}
                        confirm="Retract"
                        onConfirm={async () => {
                          await run(
                            () => admin.send(`/announcements/${s(r.id)}/retract`, "POST"),
                            phrase("Retracted."),
                          );
                          client.invalidateQueries({ queryKey: ["announcements"] });
                          client.invalidateQueries({ queryKey: ["notifications"] });
                        }}
                      >
                        {phrase("Retract")}
                      </ConfirmButton>
                    ),
                },
              ]}
            />
            <Pager page={d} />
          </Section>
        )}
      </DataState>
    </>
  );
}

const emptyProvider = {
  name: "",
  issuer: "",
  client_id: "",
  client_secret: "",
  domains: "",
  auto_create: false,
};

export function SingleSignOn() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [edit, setEdit] = useState<{ id?: string; p: typeof emptyProvider } | null>(null);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: ["sso-providers"],
    queryFn: () => admin.get<{ items: Row[] }>("/sso-providers"),
  });
  const redirect = `${window.location.origin}/admin/v1/auth/sso/callback`;
  const save = async () => {
    if (!edit) return;
    setError("");
    const body = {
      name: edit.p.name,
      issuer: edit.p.issuer,
      client_id: edit.p.client_id,
      client_secret: edit.p.client_secret,
      allowed_domains: edit.p.domains.split(/[\s,]+/).filter(Boolean),
      auto_create: edit.p.auto_create,
    };
    try {
      if (edit.id) await admin.send(`/sso-providers/${edit.id}`, "PUT", body);
      else await admin.send("/sso-providers", "POST", body);
      setEdit(null);
      client.invalidateQueries({ queryKey: ["sso-providers"] });
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const openEdit = (r: Row) =>
    setEdit({
      id: s(r.id),
      p: {
        name: s(r.name),
        issuer: s(r.issuer),
        client_id: s(r.client_id),
        client_secret: "",
        domains: ((r.allowed_domains as string[]) ?? []).join(" "),
        auto_create: r.auto_create === true,
      },
    });
  return (
    <>
      <PageHeader
        title={phrase("Single sign-on")}
        description={phrase(
          "Let people sign in with Google, Keycloak, authentik or any OpenID Connect provider. An account with an authenticator still enters its code.",
        )}
        action={
          <Button onClick={() => setEdit({ p: { ...emptyProvider } })}>
            <Plus size={16} /> {phrase("Add provider")}
          </Button>
        }
      />
      <section className="mb-4 rounded-xl border bg-card p-4">
        <h2 className="text-sm font-semibold">{phrase("Return address")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {phrase("Register this return address with the provider:")}
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <code className="min-w-0 flex-1 break-all rounded-md bg-muted px-3 py-2 text-xs">
            {redirect}
          </code>
          <CopyButton text={redirect} label={phrase("Return address")} />
        </div>
      </section>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card p-6">
            <span className="flex size-9 items-center justify-center rounded-md bg-primary/10 text-primary">
              <LogIn size={18} aria-hidden />
            </span>
            <h2 className="mt-3 text-lg font-semibold">{phrase("No providers yet")}</h2>
            <p className="mt-1 max-w-xl text-sm text-muted-foreground">
              {phrase(
                "Add the provider people already use. They sign in with that account, then enter a second step if this installation requires one.",
              )}
            </p>
            <Button className="mt-4" onClick={() => setEdit({ p: { ...emptyProvider } })}>
              <Plus size={16} /> {phrase("Add provider")}
            </Button>
          </div>
        }
      >
        {(d) => (
          <ul className="grid gap-3">
            {d.items.map((r) => {
              const domains = (r.allowed_domains as string[]) ?? [];
              return (
                <li key={s(r.id)} className="rounded-xl border bg-card p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="font-semibold">{s(r.name)}</h2>
                        <StateBadge
                          state={r.enabled ? "active" : "cancelled"}
                          label={r.enabled ? phrase("enabled") : phrase("disabled")}
                        />
                      </div>
                      <p className="mt-1 break-all text-sm text-muted-foreground">{s(r.issuer)}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      <Button size="sm" variant="outline" onClick={() => openEdit(r)}>
                        {phrase("Edit")}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={async () => {
                          await run(
                            () =>
                              admin.send(`/sso-providers/${s(r.id)}`, "PUT", {
                                name: r.name,
                                issuer: r.issuer,
                                client_id: r.client_id,
                                allowed_domains: r.allowed_domains,
                                auto_create: r.auto_create,
                                enabled: !r.enabled,
                              }),
                            r.enabled ? phrase("Disabled.") : phrase("Enabled."),
                          );
                          client.invalidateQueries({ queryKey: ["sso-providers"] });
                        }}
                      >
                        {r.enabled ? phrase("Disable") : phrase("Enable")}
                      </Button>
                      <ConfirmButton
                        title={`${phrase("Delete")} ${s(r.name)}?`}
                        description={phrase(
                          "Nobody can sign in with it any more. Accounts stay; their link to the provider is removed.",
                        )}
                        confirm="Delete provider"
                        onConfirm={async () => {
                          await run(
                            () => admin.send(`/sso-providers/${s(r.id)}`, "DELETE"),
                            phrase("Provider deleted."),
                          );
                          client.invalidateQueries({ queryKey: ["sso-providers"] });
                        }}
                      >
                        {phrase("Delete")}
                      </ConfirmButton>
                    </div>
                  </div>
                  <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-3">
                    <div>
                      <dt className="text-xs text-muted-foreground">
                        {phrase("Allowed email domains")}
                      </dt>
                      <dd className="mt-1">
                        {domains.join(", ") || phrase("Any existing account")}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-xs text-muted-foreground">{phrase("Linked users")}</dt>
                      <dd className="mt-1">{s(r.linked_users) || "0"}</dd>
                    </div>
                    <div>
                      <dt className="text-xs text-muted-foreground">{phrase("First sign-in")}</dt>
                      <dd className="mt-1">
                        {r.auto_create
                          ? phrase("Creates an account")
                          : phrase("Existing accounts only")}
                      </dd>
                    </div>
                  </dl>
                </li>
              );
            })}
          </ul>
        )}
      </DataState>
      <Dialog open={!!edit} onOpenChange={(open) => !open && setEdit(null)}>
        <DialogContent>
          <DialogTitle>{edit?.id ? phrase("Edit provider") : phrase("Add provider")}</DialogTitle>
          <DialogDescription>{phrase("Shown on the sign-in button.")}</DialogDescription>
          {edit && (
            <div className="grid max-h-[70vh] gap-3 overflow-y-auto pr-1">
              <Field label={phrase("Name")}>
                <input
                  className={inputClass}
                  value={edit.p.name}
                  onChange={(e) => setEdit({ ...edit, p: { ...edit.p, name: e.target.value } })}
                />
              </Field>
              <Field label={phrase("Issuer address")} hint="https://accounts.google.com">
                <input
                  className={inputClass}
                  value={edit.p.issuer}
                  onChange={(e) => setEdit({ ...edit, p: { ...edit.p, issuer: e.target.value } })}
                />
              </Field>
              <Field label={phrase("Client id")}>
                <input
                  className={inputClass}
                  value={edit.p.client_id}
                  onChange={(e) =>
                    setEdit({ ...edit, p: { ...edit.p, client_id: e.target.value } })
                  }
                />
              </Field>
              <Field
                label={phrase("Client secret")}
                hint={edit.id ? phrase("Leave empty to keep the saved one.") : undefined}
              >
                <input
                  type="password"
                  autoComplete="off"
                  className={inputClass}
                  value={edit.p.client_secret}
                  onChange={(e) =>
                    setEdit({ ...edit, p: { ...edit.p, client_secret: e.target.value } })
                  }
                />
              </Field>
              <Field
                label={phrase("Allowed email domains")}
                hint={phrase(
                  "Separate with spaces or commas. Empty: any domain, for existing accounts only.",
                )}
              >
                <input
                  className={inputClass}
                  value={edit.p.domains}
                  onChange={(e) => setEdit({ ...edit, p: { ...edit.p, domains: e.target.value } })}
                  placeholder="example.com"
                />
              </Field>
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={edit.p.auto_create}
                  onChange={(e) =>
                    setEdit({ ...edit, p: { ...edit.p, auto_create: e.target.checked } })
                  }
                />
                {phrase(
                  "Create an account on first sign-in (never an operator; needs allowed domains)",
                )}
              </label>
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <div className="flex gap-2">
                <Button onClick={save} disabled={!edit.p.name.trim() || !edit.p.issuer.trim()}>
                  {phrase("Save provider")}
                </Button>
                <Button variant="outline" onClick={() => setEdit(null)}>
                  {phrase("Cancel")}
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

export function DailyReportEmail() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["report-email"],
    queryFn: () => admin.get<Row>("/report-email"),
  });
  const [f, setF] = useState<{ enabled: boolean; recipients: string; sender: string } | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (q.data && !f) {
      setF({
        enabled: !!q.data.enabled,
        recipients: ((q.data.recipients as string[]) ?? []).join("\n"),
        sender: s(q.data.sender_identity_id),
      });
    }
  }, [q.data, f]);
  const save = async () => {
    if (!f) return;
    setError("");
    try {
      await admin.send("/report-email", "PUT", {
        enabled: f.enabled,
        recipients: f.recipients
          .split(/[\n,;]/)
          .map((x) => x.trim())
          .filter(Boolean),
        sender_identity_id: f.sender,
      });
      toast.success(phrase("Saved."));
      client.invalidateQueries({ queryKey: ["report-email"] });
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const senders = (q.data?.senders as Row[]) ?? [];
  return (
    <>
      <PageHeader
        title={phrase("Daily report by email")}
        description={phrase(
          "Once a day, the report for yesterday (UTC) is also sent to these addresses. It is the same report as in the notifications.",
        )}
      />
      <DataState q={q}>
        {() =>
          f && (
            <Section>
              <div className="grid max-w-xl gap-3">
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={f.enabled}
                    onChange={(e) => setF({ ...f, enabled: e.target.checked })}
                  />
                  {phrase("Send the daily report by email")}
                </label>
                <Field
                  label={phrase("Recipients")}
                  hint={phrase("One address per line. Up to 20.")}
                >
                  <textarea
                    className={textareaClass}
                    rows={4}
                    value={f.recipients}
                    onChange={(e) => setF({ ...f, recipients: e.target.value })}
                  />
                </Field>
                <Field
                  label={phrase("Send from")}
                  hint={phrase(
                    "A sender address that already exists in a project. If you choose none, the default sender of a Live environment is used.",
                  )}
                >
                  <select
                    className={selectClass}
                    value={f.sender}
                    onChange={(e) => setF({ ...f, sender: e.target.value })}
                  >
                    <option value="">
                      {phrase("Automatic")}
                      {q.data?.effective_sender ? ` (${s(q.data.effective_sender)})` : ""}
                    </option>
                    {senders.map((x) => (
                      <option key={s(x.id)} value={s(x.id)}>
                        {s(x.email)} · {s(x.project)} · {s(x.mode)}
                      </option>
                    ))}
                  </select>
                </Field>
                {!q.data?.effective_sender && (
                  <p className="text-sm text-muted-foreground">
                    {phrase(
                      "No sender address exists yet. Add a sender to a project first, otherwise nothing can be sent.",
                    )}
                  </p>
                )}
                <p className="text-xs text-muted-foreground">
                  {phrase(
                    "The email says how many messages were accepted by the mail server. That does not mean they reached an inbox.",
                  )}
                  {q.data?.last_emailed_day
                    ? ` ${phrase("Last sent for")} ${s(q.data.last_emailed_day).slice(0, 10)}.`
                    : ""}
                </p>
                {error && (
                  <p role="alert" className="text-sm text-destructive">
                    {error}
                  </p>
                )}
                <div>
                  <Button onClick={save}>{phrase("Save")}</Button>
                </div>
              </div>
            </Section>
          )
        }
      </DataState>
    </>
  );
}
