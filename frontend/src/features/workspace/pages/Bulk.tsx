import { useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useAuth } from "@/features/auth/auth";
import { useWorkspace, useSendingEnvironment } from "../context";
import { SendingMode, useSendingModeParam, withSendingMode } from "../SendingMode";
import {
  ConfirmButton,
  DataState,
  Empty,
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
  textareaClass,
  Time,
  useOffset,
  type Page,
  type Row,
} from "../ui";

type Report = {
  valid: number;
  duplicates: string[];
  invalid: string[];
  suppressed: string[];
  unsubscribed: string[];
  not_allowed: string[];
  columns: string[];
};

export function Bulk() {
  const phrase = usePhrase();
  const { to: workspaceTo } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const { api, key, info } = useSendingEnvironment(env);
  const to = (path: string) =>
    withSendingMode(workspaceTo(path), /^(bulk|messages)(\/|\?|$)/.test(path) ? env : "live");
  const navigate = useNavigate();
  const offset = useOffset();
  const q = useQuery({
    queryKey: key("campaigns", offset),
    queryFn: () => api.get<Page>(`/campaigns?limit=50&offset=${offset}`),
    refetchInterval: 15_000,
  });
  return (
    <>
      <PageHeader
        title={phrase("Bulk send")}
        description={phrase(
          "Service announcements to people who already use your product. Not for marketing to bought or scraped lists.",
        )}
        action={
          <Button asChild>
            <Link to={to("bulk/new")}>
              <Plus size={16} /> {phrase("New announcement")}
            </Link>
          </Button>
        }
      />
      <div className="mb-4">
        <SendingMode
          value={env}
          onChange={(mode) => {
            setEnv(mode);
          }}
        />
      </div>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No announcements yet")}
              description={phrase(
                "Bulk mail goes from its own verified subdomain, carries an unsubscribe link, and is paced behind transactional mail.",
              )}
              action={
                <Button asChild>
                  <Link to={to("bulk/new")}>{phrase("New announcement")}</Link>
                </Button>
              }
            />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              onRow={(r) => navigate(to(`bulk/${s(r.id)}`))}
              columns={[
                {
                  key: "name",
                  label: phrase("Name"),
                  render: (r) => (
                    <Link className="font-medium hover:underline" to={to(`bulk/${s(r.id)}`)}>
                      {s(r.name)}
                    </Link>
                  ),
                },
                { key: "sender_email", label: phrase("Sender") },
                {
                  key: "state",
                  label: phrase("State"),
                  render: (r) => <StateBadge state={s(r.state)} />,
                },
                {
                  key: "progress",
                  label: phrase("Accepted"),
                  render: (r) => `${s(r.accepted)} / ${s(r.recipients)}`,
                },
                {
                  key: "created_at",
                  label: phrase("Created"),
                  render: (r) => <Time value={r.created_at} />,
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

export function NewCampaign() {
  const phrase = usePhrase();
  const { to: workspaceTo } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const modeRef = useRef(env);
  modeRef.current = env;
  const { api, key, info } = useSendingEnvironment(env);
  const to = (path: string) =>
    withSendingMode(workspaceTo(path), /^(bulk|messages)(\/|\?|$)/.test(path) ? env : "live");
  const navigate = useNavigate();
  const client = useQueryClient();
  const senders = useQuery({
    queryKey: key("bulk-senders"),
    queryFn: () => api.get<{ items: Row[] }>("/bulk-senders"),
  });
  const templates = useQuery({
    queryKey: key("templates"),
    queryFn: () => api.get<Page>("/templates?limit=100"),
  });
  const [f, setF] = useState({
    name: "",
    sender_email: "",
    subject: "",
    html: "",
    template_id: "",
    recipients: "",
    send_at: "",
    rate_per_minute: "60",
    list_id: "",
    segment_id: "",
  });
  const [mode, setMode] = useState<"write" | "template">("write");
  const [audience, setAudience] = useState<"paste" | "list" | "segment">("paste");
  const [track, setTrack] = useState({ opens: false, clicks: false });
  const lists = useQuery({
    queryKey: key("lists"),
    queryFn: () => api.get<{ items: Row[] }>("/lists"),
  });
  const segments = useQuery({
    queryKey: key("segments"),
    queryFn: () => api.get<{ items: Row[] }>("/segments"),
  });
  const [report, setReport] = useState<Report | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f, v: string) => setF({ ...f, [k]: v });
  const sender =
    f.sender_email ||
    s(senders.data?.items.find((x) => x.is_default)?.email ?? senders.data?.items[0]?.email);
  const check = async () => {
    const r = await run(
      () => api.send<Report>("/campaigns/check", "POST", { recipients: f.recipients }),
      "",
    );
    if (r && modeRef.current === env) setReport(r);
  };
  const submit = async () => {
    const e: Record<string, string> = {};
    if (!f.name.trim()) e.name = phrase("Give it a name.");
    if (!sender) e.sender = phrase("Choose a sender.");
    if (mode === "write" && !f.subject.trim()) e.subject = phrase("Enter a subject.");
    if (mode === "write" && !f.html.trim()) e.html = phrase("Write the message.");
    if (mode === "template" && !f.template_id) e.template = phrase("Choose a template.");
    if (audience === "paste" && !f.recipients.trim()) e.recipients = phrase("Add recipients.");
    if (audience === "list" && !f.list_id) e.recipients = phrase("Choose a list.");
    if (audience === "segment" && !f.segment_id) e.recipients = phrase("Choose a segment.");
    const rate = Number(f.rate_per_minute);
    if (!Number.isInteger(rate) || rate < 1 || rate > 10000)
      e.rate = phrase("1 to 10000 per minute.");
    setErrors(e);
    if (Object.keys(e).length) return;
    setBusy(true);
    try {
      const body = {
        name: f.name.trim(),
        sender_email: sender,
        ...(audience === "paste"
          ? { recipients: f.recipients }
          : audience === "list"
            ? { list_id: f.list_id }
            : { segment_id: f.segment_id }),
        ...(mode === "write" ? { track_opens: track.opens, track_clicks: track.clicks } : {}),
        rate_per_minute: rate,
        send_at: f.send_at ? new Date(f.send_at).toISOString() : "",
        ...(mode === "write"
          ? { subject: f.subject, html: f.html.replace(/\n/g, "<br>") }
          : { template_id: f.template_id }),
      };
      const r = await api.send<{ id: string }>("/campaigns", "POST", body);
      client.invalidateQueries({ queryKey: key("campaigns") });
      navigate(to(`bulk/${r.id}`), { state: { created: true } });
    } catch (err) {
      setErrors({ form: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  };
  const noSenders = senders.isSuccess && senders.data.items.length === 0;
  return (
    <>
      <PageHeader
        back={{ to: to("bulk"), label: phrase("Bulk send") }}
        title={phrase("New announcement")}
      />
      <div className="mb-4">
        <SendingMode
          disabled={busy}
          value={env}
          onChange={(mode) => {
            setEnv(mode);
            setReport(null);
            setErrors({});
            setF((current) => ({ ...current, sender_email: "" }));
          }}
        />
      </div>
      {noSenders ? (
        <Section>
          <Empty
            title={phrase("No sender on a bulk domain")}
            description={phrase(
              "Bulk mail must come from its own subdomain, such as news.yourdomain, so problems there do not hurt transactional mail. Add that domain with “Use only for bulk announcements”, verify it, then add a sender on it.",
            )}
            action={
              <Button asChild>
                <Link to={to("domains")}>{phrase("Add a bulk domain")}</Link>
              </Button>
            }
          />
        </Section>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <Section>
            <div className="grid gap-3">
              <Field label={phrase("Name (internal)")} error={errors.name}>
                <input
                  className={inputClass}
                  value={f.name}
                  onChange={(e) => set("name", e.target.value)}
                  placeholder={phrase("November service update")}
                />
              </Field>
              <Field label={phrase("Sender")} error={errors.sender}>
                <select
                  className={selectClass}
                  value={sender}
                  onChange={(e) => set("sender_email", e.target.value)}
                >
                  {senders.data?.items.map((x) => (
                    <option key={s(x.id)} value={s(x.email)}>
                      {s(x.email)}
                    </option>
                  ))}
                </select>
              </Field>
              <div className="flex gap-2 border-b" role="tablist">
                {(["write", "template"] as const).map((m) => (
                  <button
                    key={m}
                    type="button"
                    role="tab"
                    aria-selected={mode === m}
                    onClick={() => setMode(m)}
                    className={`-mb-px border-b-2 px-3 py-2 text-sm ${mode === m ? "border-primary font-semibold" : "border-transparent text-muted-foreground"}`}
                  >
                    {m === "write" ? phrase("Write") : phrase("Use a template")}
                  </button>
                ))}
              </div>
              {mode === "write" ? (
                <>
                  <Field label={phrase("Subject")} error={errors.subject}>
                    <input
                      className={inputClass}
                      value={f.subject}
                      onChange={(e) => set("subject", e.target.value)}
                    />
                  </Field>
                  <Field
                    label={phrase("Message")}
                    error={errors.html}
                    hint={phrase(
                      "Use {{name}} for a CSV column. An unsubscribe link is added at the end.",
                    )}
                  >
                    <textarea
                      className={textareaClass}
                      rows={8}
                      value={f.html}
                      onChange={(e) => set("html", e.target.value)}
                    />
                  </Field>
                  <fieldset className="grid gap-1 text-sm">
                    <legend className="mb-1 font-medium">
                      {phrase("Tracking (off by default)")}
                    </legend>
                    <label className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={track.opens}
                        onChange={(e) => setTrack({ ...track, opens: e.target.checked })}
                      />
                      {phrase("Count opens (an estimate: some mail apps load images on their own)")}
                    </label>
                    <label className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={track.clicks}
                        onChange={(e) => setTrack({ ...track, clicks: e.target.checked })}
                      />
                      {phrase("Count link clicks")}
                    </label>
                  </fieldset>
                </>
              ) : (
                <Field
                  label={phrase("Template")}
                  error={errors.template}
                  hint={phrase(
                    "The template must declare a text variable unsubscribe_url and show it; other variables come from CSV columns.",
                  )}
                >
                  <select
                    className={selectClass}
                    value={f.template_id}
                    onChange={(e) => set("template_id", e.target.value)}
                  >
                    <option value="">{phrase("Choose…")}</option>
                    {templates.data?.items
                      .filter((t) => t.published_version)
                      .map((t) => (
                        <option key={s(t.id)} value={s(t.id)}>
                          {s(t.slug)}
                        </option>
                      ))}
                  </select>
                </Field>
              )}
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={phrase("Send at (optional)")}>
                  <input
                    type="datetime-local"
                    className={inputClass}
                    value={f.send_at}
                    onChange={(e) => set("send_at", e.target.value)}
                  />
                </Field>
                <Field
                  label={phrase("Rate per minute")}
                  error={errors.rate}
                  hint={phrase("Bulk mail waits behind transactional mail.")}
                >
                  <input
                    type="number"
                    min={1}
                    max={10000}
                    className={inputClass}
                    value={f.rate_per_minute}
                    onChange={(e) => set("rate_per_minute", e.target.value)}
                  />
                </Field>
              </div>
            </div>
          </Section>
          <Section title={phrase("Recipients")}>
            <div className="mb-3 flex gap-2 border-b" role="tablist">
              {(
                [
                  ["paste", "Paste or upload"],
                  ["list", "A list"],
                  ["segment", "A segment"],
                ] as const
              ).map(([a, label]) => (
                <button
                  key={a}
                  type="button"
                  role="tab"
                  aria-selected={audience === a}
                  onClick={() => {
                    setAudience(a);
                    setReport(null);
                  }}
                  className={`-mb-px border-b-2 px-3 py-2 text-sm ${audience === a ? "border-primary font-semibold" : "border-transparent text-muted-foreground"}`}
                >
                  {phrase(label)}
                </button>
              ))}
            </div>
            {audience !== "paste" ? (
              <Field
                label={audience === "list" ? phrase("List") : phrase("Segment")}
                error={errors.recipients}
                hint={phrase(
                  "Only contacts who can receive marketing mail are included. Their name and attributes are the variables.",
                )}
              >
                <select
                  className={selectClass}
                  value={audience === "list" ? f.list_id : f.segment_id}
                  onChange={(e) =>
                    set(audience === "list" ? "list_id" : "segment_id", e.target.value)
                  }
                >
                  <option value="">{phrase("Choose…")}</option>
                  {(audience === "list" ? lists.data?.items : segments.data?.items)?.map((x) => (
                    <option key={s(x.id)} value={s(x.id)}>
                      {s(x.name)}
                      {audience === "list" ? ` (${s(x.members)})` : ""}
                    </option>
                  ))}
                </select>
              </Field>
            ) : (
              <>
                <Field
                  label={phrase("Paste addresses, or a CSV with an “email” column")}
                  error={errors.recipients}
                >
                  <textarea
                    className={textareaClass + " font-mono text-xs"}
                    rows={10}
                    value={f.recipients}
                    onChange={(e) => {
                      set("recipients", e.target.value);
                      setReport(null);
                    }}
                    placeholder={"email,name\nasha@example.org,Asha"}
                  />
                </Field>
                <div className="mt-2 flex flex-wrap gap-2">
                  <label className="inline-flex cursor-pointer items-center gap-1 rounded-md border px-3 py-1.5 text-sm hover:bg-muted">
                    <Upload size={14} /> {phrase("Upload CSV")}
                    <input
                      type="file"
                      accept=".csv,text/csv,text/plain"
                      className="sr-only"
                      onChange={async (e) => {
                        const file = e.target.files?.[0];
                        if (file) {
                          set("recipients", await file.text());
                          setReport(null);
                        }
                      }}
                    />
                  </label>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!f.recipients.trim()}
                    onClick={check}
                  >
                    {phrase("Check recipients")}
                  </Button>
                </div>
              </>
            )}
            {report && (
              <div role="status" className="mt-3 space-y-2 text-sm">
                <p className="text-lg font-semibold">
                  {report.valid} {phrase("unique valid addresses")}
                  {info && report.valid > info.bulk_max_per_campaign && (
                    <span className="block text-sm text-destructive">
                      {phrase("Above this project's limit of")} {info.bulk_max_per_campaign}
                    </span>
                  )}
                </p>
                {report.columns.length > 0 && (
                  <p className="text-xs text-muted-foreground">
                    {phrase("Variables from CSV:")}{" "}
                    {report.columns.map((c) => `{{${c}}}`).join(", ")}
                  </p>
                )}
                {(
                  [
                    ["duplicates", "Duplicates (sent once)"],
                    ["invalid", "Invalid"],
                    ["suppressed", "Suppressed"],
                    ["unsubscribed", "Unsubscribed from announcements"],
                    ["not_allowed", "Not on the Live approved list"],
                  ] as const
                ).map(([k, label]) =>
                  report[k].length ? (
                    <details key={k}>
                      <summary className="cursor-pointer">
                        {phrase(label)}: {report[k].length}
                      </summary>
                      <ul className="mt-1 max-h-32 overflow-auto pl-4 text-xs text-muted-foreground">
                        {report[k].map((x) => (
                          <li key={x}>{x}</li>
                        ))}
                      </ul>
                    </details>
                  ) : null,
                )}
              </div>
            )}
          </Section>
          <div className="lg:col-span-2">
            {errors.form && (
              <p role="alert" className="mb-2 text-sm text-destructive">
                {errors.form}
              </p>
            )}
            <Button onClick={submit} disabled={busy}>
              {busy ? phrase("Saving…") : phrase("Save as draft")}
            </Button>
            <span className="ml-3 text-xs text-muted-foreground">
              {phrase("Nothing is sent until you press Start on the next page.")}
            </span>
          </div>
        </div>
      )}
    </>
  );
}

export function CampaignDetail() {
  const phrase = usePhrase();
  const { id = "" } = useParams();
  const { to: workspaceTo } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const { api, key, info } = useSendingEnvironment(env);
  const to = (path: string) =>
    withSendingMode(workspaceTo(path), /^(bulk|messages)(\/|\?|$)/.test(path) ? env : "live");
  const { user } = useAuth();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: key("campaign", id),
    queryFn: () => api.get<Row>(`/campaigns/${id}`),
    refetchInterval: 10_000,
  });
  const act = async (action: string, msg: string) => {
    await run(() => api.send(`/campaigns/${id}/${action}`, "POST"), msg);
    client.invalidateQueries({ queryKey: key("campaign", id) });
    client.invalidateQueries({ queryKey: key("campaigns") });
  };
  const canAct = Boolean(user);
  return (
    <DataState q={q}>
      {(c) => {
        const state = s(c.state);
        const totals: [string, unknown, string?][] = [
          ["Recipients", c.recipients],
          ["Waiting", c.pending],
          ["Accepted into the queue", c.accepted, `messages?campaign=${id}`],
          ["Submitted", c.submitted, `messages?campaign=${id}&state=submitted`],
          [
            "Failed",
            Number(c.failed) + Number(c.send_failed),
            `messages?campaign=${id}&state=failed`,
          ],
          ["Skipped", c.skipped],
          ["Bounced", c.bounced],
          ["Complaints", c.complaints, "complaints"],
          ["Unsubscribed", c.unsubscribed, "suppressions?tab=unsubscribes"],
        ];
        return (
          <>
            <PageHeader
              back={{ to: to("bulk"), label: phrase("Bulk send") }}
              title={s(c.name)}
              description={
                <>
                  <StateBadge state={env} /> <StateBadge state={state} /> {s(c.sender_email)} ·{" "}
                  {s(c.rate_per_minute)} {phrase("per minute")}
                  {c.send_at ? (
                    <>
                      {" · "}
                      {phrase("starts")} <Time value={c.send_at} />
                    </>
                  ) : null}
                </>
              }
              action={
                canAct && (
                  <>
                    {state === "draft" && (
                      <ConfirmButton
                        size="default"
                        variant={env === "live" ? "destructive" : "default"}
                        destructive={env === "live"}
                        title={phrase("Start sending?")}
                        description={`${s(c.recipients)} ${phrase("recipients will get this announcement")} ${env === "live" ? phrase("for real (Live).") : phrase("in the Sandbox (captured only).")}`}
                        confirm="Start"
                        onConfirm={() => act("start", phrase("Started."))}
                      >
                        {phrase("Start")}
                      </ConfirmButton>
                    )}
                    {(state === "sending" || state === "scheduled") && (
                      <Button variant="outline" onClick={() => act("pause", phrase("Paused."))}>
                        {phrase("Pause")}
                      </Button>
                    )}
                    {state === "paused" && (
                      <Button onClick={() => act("resume", phrase("Resumed."))}>
                        {phrase("Resume")}
                      </Button>
                    )}
                    {["draft", "scheduled", "sending", "paused"].includes(state) && (
                      <ConfirmButton
                        size="default"
                        title={phrase("Cancel this announcement?")}
                        description={phrase(
                          "Recipients not reached yet will not get it, and queued messages are cancelled. This cannot be undone.",
                        )}
                        confirm="Cancel announcement"
                        onConfirm={() => act("cancel", phrase("Cancelled."))}
                      >
                        {phrase("Cancel")}
                      </ConfirmButton>
                    )}
                  </>
                )
              }
            />
            {state === "paused" && s(c.pause_reason) && (
              <p
                role="alert"
                className="mb-4 rounded-lg border border-warning/50 bg-warning/10 p-3 text-sm"
              >
                {phrase("Paused:")} {s(c.pause_reason)}
              </p>
            )}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
              {totals.map(([label, v, link]) => {
                const body = (
                  <>
                    <p className="text-xs text-muted-foreground">{phrase(label)}</p>
                    <p className="mt-1 text-2xl font-bold">{s(v) || "0"}</p>
                  </>
                );
                return link ? (
                  <Link
                    key={label}
                    to={to(link)}
                    className="rounded-xl border bg-card p-3 hover:border-primary"
                  >
                    {body}
                  </Link>
                ) : (
                  <div key={label} className="rounded-xl border bg-card p-3">
                    {body}
                  </div>
                );
              })}
            </div>
            {(c.list_name || c.segment_name) && (
              <p className="mt-3 text-sm text-muted-foreground">
                {phrase("Audience:")}{" "}
                {c.list_name
                  ? `${phrase("list")} ${s(c.list_name)}`
                  : `${phrase("segment")} ${s(c.segment_name)}`}
              </p>
            )}
            {Boolean(c.tracking) && <TrackingStats t={c.tracking as Row} />}
            {((c.skip_reasons as Row[]) ?? []).length > 0 && (
              <Section className="mt-4" title={phrase("Why recipients were skipped or failed")}>
                <ul className="text-sm">
                  {(c.skip_reasons as Row[]).map((r) => (
                    <li key={s(r.reason)}>
                      {s(r.reason).replace(/_/g, " ")}: {s(r.n)}
                    </li>
                  ))}
                </ul>
              </Section>
            )}
            <Section className="mt-4" title={phrase("Content")}>
              {c.template ? (
                <p className="text-sm">
                  {phrase("Template")}{" "}
                  <Link className="text-primary" to={to(`templates?id=${s(c.template_id)}`)}>
                    {s(c.template)}
                  </Link>
                </p>
              ) : (
                <>
                  <p className="font-semibold">{s(c.subject)}</p>
                  <pre className="mt-2 whitespace-pre-wrap font-sans text-sm text-muted-foreground">
                    {s(c.text_body)}
                  </pre>
                </>
              )}
            </Section>
          </>
        );
      }}
    </DataState>
  );
}

function TrackingStats({ t }: { t: Row }) {
  const phrase = usePhrase();
  const links = (t.links as Row[]) ?? [];
  return (
    <Section className="mt-4" title={phrase("Opens and clicks")}>
      <div className="grid grid-cols-2 gap-3 sm:max-w-md">
        <div className="rounded-xl border p-3">
          <p className="text-xs text-muted-foreground">{phrase("Unique opens (estimate)")}</p>
          <p className="mt-1 text-2xl font-bold">{s(t.unique_opens) || "0"}</p>
        </div>
        <div className="rounded-xl border p-3">
          <p className="text-xs text-muted-foreground">{phrase("Unique clicks")}</p>
          <p className="mt-1 text-2xl font-bold">{s(t.unique_clicks) || "0"}</p>
        </div>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {phrase(
          "Opens are an estimate: some mail apps load images on their own, others block them.",
        )}
      </p>
      {links.length > 0 && (
        <Table
          rows={links}
          rowKey={(r) => s(r.idx)}
          columns={[
            {
              key: "url",
              label: phrase("Link"),
              render: (r) => <span className="break-all">{s(r.url)}</span>,
            },
            { key: "clicks", label: phrase("Clicks") },
          ]}
        />
      )}
    </Section>
  );
}
