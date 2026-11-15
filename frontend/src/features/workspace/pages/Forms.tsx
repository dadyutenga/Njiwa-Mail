import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Download, Paperclip, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useSendingEnvironment } from "../context";
import { SendingMode, useSendingModeParam } from "../SendingMode";
import {
  ConfirmButton,
  CopyButton,
  DataState,
  Empty,
  errorMessage,
  Field,
  inputClass,
  n,
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
  useParam,
  type Page,
  type Row,
} from "../ui";

function useFormsEnv() {
  const [env, setEnv] = useSendingModeParam();
  const ctx = useSendingEnvironment(env);
  return { env, setEnv, ...ctx };
}

const lines = (v: string) =>
  v
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean);

const actionState: Record<string, string> = {
  accept: "active",
  flag: "pending",
  quarantine: "failed",
  reject: "failed",
};

// ---------------------------------------------------------------- forms

type FormDraft = {
  name: string;
  origins: string;
  emails: string;
  digest: string;
  redirect_url: string;
  reply_from: string;
  require_nonce: boolean;
  enabled: boolean;
};

const blank: FormDraft = {
  name: "",
  origins: "",
  emails: "",
  digest: "immediate",
  redirect_url: "",
  reply_from: "",
  require_nonce: true,
  enabled: true,
};

function snippet(id: string, nonce: boolean) {
  const url = `${window.location.origin}/v1/forms/${id}`;
  return `<form action="${url}" method="post">
  <input name="email" type="email" placeholder="Email" required>
  <textarea name="message" placeholder="Message" required></textarea>
  <input name="hp_website" tabindex="-1" autocomplete="off" style="position:absolute;left:-9999px" aria-hidden="true">${
    nonce
      ? `
  <input type="hidden" name="_nonce" id="njiwa-nonce">
  <script>
    fetch("${url}/nonce").then(r => r.json()).then(j => {
      document.getElementById("njiwa-nonce").value = j.nonce;
    });
  </script>`
      : ""
  }
  <button type="submit">Send</button>
</form>`;
}

export function Forms() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useFormsEnv();
  const client = useQueryClient();
  const [editing, setEditing] = useState<{ id?: string; draft: FormDraft } | null>(null);
  const [embed, setEmbed] = useState<Row | null>(null);
  const [rulesFor, setRulesFor] = useState<Row | null>(null);
  const list = useQuery({
    queryKey: key("forms"),
    queryFn: () => api.get<{ items: Row[] }>("/forms"),
  });
  const refresh = () => client.invalidateQueries({ queryKey: key("forms") });
  return (
    <>
      <PageHeader
        title={phrase("Forms")}
        description={phrase(
          "Receive messages from your website. Every submission is checked for spam, stored encrypted and shown in the form inbox.",
        )}
        action={
          <Button onClick={() => setEditing({ draft: blank })}>
            <Plus size={16} /> {phrase("New form")}
          </Button>
        }
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} />
      </div>
      <DataState
        q={list}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No forms")}
              description={phrase(
                "Create a form, list the websites allowed to post to it, then paste the snippet into your page.",
              )}
              action={
                <Button onClick={() => setEditing({ draft: blank })}>{phrase("New form")}</Button>
              }
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              { key: "name", label: phrase("Name"), render: (r) => <b>{s(r.name)}</b> },
              {
                key: "enabled",
                label: phrase("Status"),
                render: (r) => (
                  <StateBadge
                    state={r.enabled ? "active" : "cancelled"}
                    label={phrase(r.enabled ? "Enabled" : "Disabled")}
                  />
                ),
              },
              { key: "submissions", label: phrase("Submissions") },
              { key: "unread", label: phrase("Unread") },
              { key: "digest", label: phrase("Notify"), render: (r) => phrase(s(r.digest)) },
              {
                key: "id",
                label: "",
                render: (r) => (
                  <span className="flex flex-wrap justify-end gap-2">
                    <Button size="sm" variant="outline" onClick={() => setEmbed(r)}>
                      {phrase("Embed")}
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => setRulesFor(r)}>
                      {phrase("Rules")}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        setEditing({
                          id: s(r.id),
                          draft: {
                            name: s(r.name),
                            origins: ((r.allowed_origins as string[]) ?? []).join("\n"),
                            emails: ((r.notify_emails as string[]) ?? []).join("\n"),
                            digest: s(r.digest) || "immediate",
                            redirect_url: s(r.redirect_url),
                            reply_from: s(r.reply_from),
                            require_nonce: !!r.require_nonce,
                            enabled: !!r.enabled,
                          },
                        })
                      }
                    >
                      {phrase("Edit")}
                    </Button>
                    <ConfirmButton
                      title={phrase("Delete this form?")}
                      description={phrase(
                        "Its submissions and files are deleted permanently and the form stops accepting messages.",
                      )}
                      confirm="Delete"
                      onConfirm={async () => {
                        await run(
                          () => api.send(`/forms/${s(r.id)}`, "DELETE"),
                          phrase("Form deleted"),
                        );
                        refresh();
                      }}
                    >
                      <Trash2 size={14} />
                    </ConfirmButton>
                  </span>
                ),
              },
            ]}
          />
        )}
      </DataState>
      <FormDialog
        state={editing}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          refresh();
        }}
        api={api}
      />
      <FormRulesDialog form={rulesFor} onClose={() => setRulesFor(null)} api={api} />
      <Dialog open={!!embed} onOpenChange={(v) => !v && setEmbed(null)}>
        <DialogContent className="max-w-2xl">
          <DialogTitle>{phrase("Embed on your website")}</DialogTitle>
          <DialogDescription>
            {phrase(
              "Paste this into a page served from one of the allowed origins. Keep the hidden hp_website field: real visitors never fill it, bots do.",
            )}
          </DialogDescription>
          {embed && (
            <>
              <pre className="max-h-80 overflow-auto rounded-lg border bg-muted p-3 text-xs">
                {snippet(s(embed.id), !!embed.require_nonce)}
              </pre>
              <div className="flex justify-end">
                <CopyButton text={snippet(s(embed.id), !!embed.require_nonce)} />
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function FormDialog({
  state,
  onClose,
  onSaved,
  api,
}: {
  state: { id?: string; draft: FormDraft } | null;
  onClose: () => void;
  onSaved: () => void;
  api: ReturnType<typeof useSendingEnvironment>["api"];
}) {
  const phrase = usePhrase();
  const [f, setF] = useState<FormDraft>(blank);
  const [error, setError] = useState("");
  useEffect(() => {
    if (state) {
      setF(state.draft);
      setError("");
    }
  }, [state]);
  const save = async () => {
    setError("");
    const body = {
      name: f.name.trim(),
      allowed_origins: lines(f.origins),
      notify_emails: lines(f.emails),
      digest: f.digest,
      redirect_url: f.redirect_url.trim(),
      reply_from: f.reply_from.trim(),
      require_nonce: f.require_nonce,
      enabled: f.enabled,
    };
    try {
      if (state?.id) await api.send(`/forms/${state.id}`, "PUT", body);
      else await api.send("/forms", "POST", body);
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Dialog open={!!state} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogTitle>{phrase(state?.id ? "Edit form" : "New form")}</DialogTitle>
        <DialogDescription>
          {phrase("Only the websites you list can post to this form.")}
        </DialogDescription>
        <div className="grid gap-3">
          <Field label={phrase("Name")}>
            <input
              className={inputClass}
              value={f.name}
              onChange={(e) => setF({ ...f, name: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Allowed websites")}
            hint={phrase("One per line, for example https://www.example.com")}
          >
            <textarea
              className={textareaClass}
              rows={3}
              value={f.origins}
              onChange={(e) => setF({ ...f, origins: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Notify these addresses")}
            hint={phrase("One per line. They are told when a message arrives.")}
          >
            <textarea
              className={textareaClass}
              rows={2}
              value={f.emails}
              onChange={(e) => setF({ ...f, emails: e.target.value })}
            />
          </Field>
          <Field label={phrase("Notification timing")}>
            <select
              className={selectClass}
              value={f.digest}
              onChange={(e) => setF({ ...f, digest: e.target.value })}
            >
              {["immediate", "hourly", "daily"].map((x) => (
                <option key={x} value={x}>
                  {phrase(x)}
                </option>
              ))}
            </select>
          </Field>
          <Field
            label={phrase("Thank-you page")}
            hint={phrase("Optional. Where visitors go after sending.")}
          >
            <input
              className={inputClass}
              type="url"
              value={f.redirect_url}
              onChange={(e) => setF({ ...f, redirect_url: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Reply from")}
            hint={phrase("A verified sender address on your domain, used for replies.")}
          >
            <input
              className={inputClass}
              type="email"
              value={f.reply_from}
              onChange={(e) => setF({ ...f, reply_from: e.target.value })}
            />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={f.require_nonce}
              onChange={(e) => setF({ ...f, require_nonce: e.target.checked })}
            />
            {phrase("Require a one-time token (blocks scripts that skip the page)")}
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={f.enabled}
              onChange={(e) => setF({ ...f, enabled: e.target.checked })}
            />
            {phrase("Accept submissions")}
          </label>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={onClose}>
              {phrase("Cancel")}
            </Button>
            <Button onClick={save} disabled={!f.name.trim()}>
              {phrase("Save")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- inbox

const states = ["new", "open", "replied", "closed", "spam"];
const priorities = ["high", "normal", "low"];
const priorityState: Record<string, string> = {
  high: "failed",
  normal: "cancelled",
  low: "cancelled",
};

export function FormInbox() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useFormsEnv();
  const { project } = useWorkspace();
  const client = useQueryClient();
  const [form, setForm] = useParam("form");
  const [state, setState] = useParam("state");
  const [tag, setTag] = useParam("tag");
  const [priority, setPriority] = useParam("priority");
  const [open, setOpen] = useParam("m");
  const offset = useOffset();
  const forms = useQuery({
    queryKey: key("forms"),
    queryFn: () => api.get<{ items: Row[] }>("/forms"),
  });
  const params = new URLSearchParams({ limit: "50", offset: String(offset) });
  if (form) params.set("form", form);
  if (state) params.set("state", state);
  if (tag) params.set("tag", tag);
  if (priority) params.set("priority", priority);
  const exportHref = `/admin/v1${api.base}/forms/submissions/export?${(() => {
    const q = new URLSearchParams(params);
    q.delete("limit");
    q.delete("offset");
    return q;
  })()}`;
  const summary = useQuery({
    queryKey: key("form-summary"),
    queryFn: () => api.get<Row>("/forms/submissions/summary?days=30"),
  });
  const page = useQuery({
    queryKey: key("form-submissions", params.toString()),
    queryFn: () => api.get<Page>(`/forms/submissions?${params}`),
  });
  // New submissions arrive live (server-sent events); the browser resumes by itself.
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const es = new EventSource(
      `/admin/v1/w/${encodeURIComponent(project)}/${env}/forms/submissions/stream`,
      { withCredentials: true },
    );
    es.addEventListener("submission", () => {
      client.invalidateQueries({ queryKey: key("form-submissions") });
      client.invalidateQueries({ queryKey: key("forms") });
    });
    return () => es.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, env]);
  return (
    <>
      <PageHeader
        title={phrase("Form inbox")}
        description={phrase(
          "Messages sent through your website forms. Suspected spam is kept apart and never notifies you.",
        )}
      />
      {summary.data && (
        <div
          className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-5"
          aria-label={phrase("Last 30 days")}
        >
          {(
            [
              ["submissions", "Submissions"],
              ["unread", "Unread"],
              ["high_priority_open", "High priority, open"],
              ["replied", "Replied"],
              ["spam", "Spam"],
            ] as const
          ).map(([k, label]) => (
            <div key={k} className="rounded-xl border bg-card p-3">
              <div className="text-xs text-muted-foreground">{phrase(label)}</div>
              <div className="text-xl font-semibold">
                {n((summary.data.totals as Row | undefined)?.[k])}
              </div>
            </div>
          ))}
        </div>
      )}
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <SendingMode value={env} onChange={setEnv} />
        <Field label={phrase("Tag")}>
          <input
            className={inputClass}
            value={tag}
            onChange={(e) => setTag(e.target.value.trim())}
            placeholder={phrase("Any")}
          />
        </Field>
        <Field label={phrase("Priority")}>
          <select
            className={selectClass}
            value={priority}
            onChange={(e) => setPriority(e.target.value)}
          >
            <option value="">{phrase("Any")}</option>
            {priorities.map((x) => (
              <option key={x} value={x}>
                {phrase(x)}
              </option>
            ))}
          </select>
        </Field>
        <Button variant="outline" asChild>
          <a href={exportHref} download>
            <Download size={14} /> {phrase("Export CSV")}
          </a>
        </Button>
        <Field label={phrase("Form")}>
          <select className={selectClass} value={form} onChange={(e) => setForm(e.target.value)}>
            <option value="">{phrase("All")}</option>
            {forms.data?.items.map((x) => (
              <option key={s(x.id)} value={s(x.id)}>
                {s(x.name)}
              </option>
            ))}
          </select>
        </Field>
        <Field label={phrase("State")}>
          <select className={selectClass} value={state} onChange={(e) => setState(e.target.value)}>
            <option value="">{phrase("Open items")}</option>
            {states.map((x) => (
              <option key={x} value={x}>
                {phrase(x)}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <DataState
        q={page}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No submissions")}
              description={phrase("Messages sent through your forms appear here as they arrive.")}
            />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                {
                  key: "sender_masked",
                  label: phrase("From"),
                  render: (r) => (
                    <button
                      className="text-left font-medium text-primary"
                      onClick={() => setOpen(s(r.id))}
                    >
                      {s(r.sender_masked) || phrase("Unknown")}
                    </button>
                  ),
                },
                { key: "form", label: phrase("Form") },
                {
                  key: "preview",
                  label: phrase("Message"),
                  render: (r) => (
                    <span className="flex items-center gap-1">
                      {n(r.files) > 0 && <Paperclip size={13} aria-label={phrase("has files")} />}
                      <span className="line-clamp-1">{s(r.preview)}</span>
                    </span>
                  ),
                },
                {
                  key: "priority",
                  label: phrase("Priority"),
                  render: (r) => (
                    <StateBadge
                      state={priorityState[s(r.priority)] ?? "cancelled"}
                      label={phrase(s(r.priority) || "normal")}
                    />
                  ),
                },
                {
                  key: "tags",
                  label: phrase("Tags"),
                  render: (r) => (
                    <span className="flex flex-wrap gap-1">
                      {((r.tags as string[]) ?? []).map((t) => (
                        <button
                          key={t}
                          className="rounded bg-muted px-1.5 py-0.5 text-xs"
                          onClick={() => setTag(t)}
                        >
                          {t}
                        </button>
                      ))}
                    </span>
                  ),
                },
                {
                  key: "state",
                  label: phrase("State"),
                  render: (r) => (
                    <StateBadge
                      state={s(r.state) === "new" ? "pending" : "active"}
                      label={phrase(s(r.state))}
                    />
                  ),
                },
                {
                  key: "action",
                  label: phrase("Spam check"),
                  render: (r) => (
                    <StateBadge
                      state={actionState[s(r.action)] ?? "cancelled"}
                      label={`${phrase(s(r.action))} · ${n(r.spam_score)}`}
                    />
                  ),
                },
                {
                  key: "received_at",
                  label: phrase("Received"),
                  render: (r) => <Time value={r.received_at} relative />,
                },
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
      <SubmissionDialog id={open} onClose={() => setOpen("")} />
    </>
  );
}

function SubmissionDialog({ id, onClose }: { id: string; onClose: () => void }) {
  const phrase = usePhrase();
  const { env, api, key } = useFormsEnv();
  const client = useQueryClient();
  const [subject, setSubject] = useState("");
  const [text, setText] = useState("");
  const [tags, setTags] = useState("");
  const [block, setBlock] = useState(false);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: key("form-submission", id),
    queryFn: () => api.get<Row>(`/forms/submissions/${id}`),
    enabled: !!id,
  });
  useEffect(() => {
    setSubject("");
    setText("");
    setError("");
    setBlock(false);
  }, [id]);
  useEffect(() => {
    setTags(((q.data?.tags as string[]) ?? []).join(", "));
  }, [q.data]);
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("form-submission", id) });
    client.invalidateQueries({ queryKey: key("form-submissions") });
    client.invalidateQueries({ queryKey: key("form-summary") });
    client.invalidateQueries({ queryKey: key("forms") });
  };
  const label = async (body: Record<string, unknown>) => {
    await run(() => api.send(`/forms/submissions/${id}`, "PATCH", body), phrase("Updated"));
    refresh();
  };
  const markSpam = async () => {
    await run(
      () => api.send(`/forms/submissions/${id}/spam`, "POST", { block_sender: block }),
      phrase("Marked as spam"),
    );
    refresh();
  };
  const notSpam = async () => {
    await run(
      () => api.send(`/forms/submissions/${id}/not-spam`, "POST"),
      phrase("Moved back to the inbox"),
    );
    refresh();
  };
  const setState = async (state: string) => {
    await run(() => api.send(`/forms/submissions/${id}`, "PATCH", { state }), phrase("Updated"));
    refresh();
  };
  const reply = async () => {
    setError("");
    try {
      await api.send(`/forms/submissions/${id}/reply`, "POST", { subject, text });
      setText("");
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const d = q.data;
  const fields = (d?.fields as Record<string, string>) ?? {};
  const files = (d?.files as Row[]) ?? [];
  return (
    <Dialog open={!!id} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogTitle>{phrase("Submission")}</DialogTitle>
        <DialogDescription>
          {d ? `${s(d.form)} · ${s(d.sender_masked)}` : phrase("Loading")}
        </DialogDescription>
        <DataState q={q}>
          {() => (
            <div className="grid gap-4">
              <Section>
                <dl className="grid gap-2 text-sm">
                  {Object.entries(fields).map(([k, v]) => (
                    <div key={k}>
                      <dt className="text-xs uppercase text-muted-foreground">{k}</dt>
                      <dd className="whitespace-pre-wrap break-words">{v}</dd>
                    </div>
                  ))}
                </dl>
              </Section>
              {files.length > 0 && (
                <Section title={phrase("Files")}>
                  <ul className="grid gap-1 text-sm">
                    {files.map((f) => (
                      <li key={s(f.id)}>
                        <a
                          className="text-primary underline"
                          href={`/admin/v1${api.base}/forms/submissions/${id}/files/${s(f.id)}`}
                        >
                          {s(f.filename)}
                        </a>{" "}
                        <span className="text-muted-foreground">({n(f.size)} B)</span>
                      </li>
                    ))}
                  </ul>
                </Section>
              )}
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <StateBadge
                  state={actionState[s(d?.action)] ?? "cancelled"}
                  label={`${phrase(s(d?.action))} · ${n(d?.spam_score)}`}
                />
                {((d?.spam_reasons as string[]) ?? []).map((r) => (
                  <span key={r} className="rounded bg-muted px-2 py-0.5 text-xs">
                    {r}
                  </span>
                ))}
              </div>
              <Section title={phrase("Priority and tags")}>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label={phrase("Priority")}>
                    <select
                      className={selectClass}
                      value={s(d?.priority) || "normal"}
                      onChange={(e) => label({ priority: e.target.value })}
                    >
                      {priorities.map((x) => (
                        <option key={x} value={x}>
                          {phrase(x)}
                        </option>
                      ))}
                    </select>
                  </Field>
                  <Field
                    label={phrase("Tags")}
                    hint={phrase("Separate with commas, up to 10.")}
                    className="sm:col-span-2"
                  >
                    <span className="flex gap-2">
                      <input
                        className={inputClass}
                        value={tags}
                        onChange={(e) => setTags(e.target.value)}
                      />
                      <Button variant="outline" onClick={() => label({ tags: lines(tags) })}>
                        {phrase("Save")}
                      </Button>
                    </span>
                  </Field>
                </div>
              </Section>
              <div className="flex flex-wrap items-center gap-2">
                {s(d?.state) === "spam" ? (
                  <Button size="sm" variant="outline" onClick={notSpam}>
                    {phrase("Not spam")}
                  </Button>
                ) : (
                  <>
                    <Button size="sm" variant="outline" onClick={markSpam}>
                      {phrase("Mark as spam")}
                    </Button>
                    <label className="flex items-center gap-1 text-xs">
                      <input
                        type="checkbox"
                        checked={block}
                        onChange={(e) => setBlock(e.target.checked)}
                      />
                      {phrase("Also block this sender")}
                    </label>
                  </>
                )}
              </div>
              <div className="flex flex-wrap gap-2">
                {states.map((x) => (
                  <Button
                    key={x}
                    size="sm"
                    variant={s(d?.state) === x ? "default" : "outline"}
                    onClick={() => setState(x)}
                  >
                    {phrase(x)}
                  </Button>
                ))}
                <ConfirmButton
                  title={phrase("Delete this submission?")}
                  description={phrase("The message and its files are deleted permanently.")}
                  confirm="Delete"
                  onConfirm={async () => {
                    await run(
                      () => api.send(`/forms/submissions/${id}`, "DELETE"),
                      phrase("Submission deleted"),
                    );
                    refresh();
                    onClose();
                  }}
                >
                  <Trash2 size={14} />
                </ConfirmButton>
              </div>
              {env && fields.email && (
                <Section title={phrase("Reply by email")}>
                  <div className="grid gap-3">
                    <p className="text-xs text-muted-foreground">
                      {phrase(
                        "The reply is accepted for sending. Delivery is confirmed only by the message timeline.",
                      )}
                    </p>
                    <Field label={phrase("Subject")}>
                      <input
                        className={inputClass}
                        value={subject}
                        onChange={(e) => setSubject(e.target.value)}
                      />
                    </Field>
                    <Field label={phrase("Message")}>
                      <textarea
                        className={textareaClass}
                        rows={5}
                        value={text}
                        onChange={(e) => setText(e.target.value)}
                      />
                    </Field>
                    {error && (
                      <p role="alert" className="text-sm text-destructive">
                        {error}
                      </p>
                    )}
                    <div className="flex justify-end">
                      <Button onClick={reply} disabled={!text.trim() || !subject.trim()}>
                        {phrase("Send reply")}
                      </Button>
                    </div>
                  </div>
                </Section>
              )}
            </div>
          )}
        </DataState>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- routing rules

const ruleOps = ["contains", "equals", "regex", "domain", "exists"];

type RuleDraft = {
  name: string;
  field: string;
  op: string;
  value: string;
  set_priority: string;
  add_tags: string;
  stop: boolean;
};

const blankRule: RuleDraft = {
  name: "",
  field: "",
  op: "contains",
  value: "",
  set_priority: "",
  add_tags: "",
  stop: false,
};

function FormRulesDialog({
  form,
  onClose,
  api,
}: {
  form: Row | null;
  onClose: () => void;
  api: ReturnType<typeof useSendingEnvironment>["api"];
}) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const id = form ? s(form.id) : "";
  const [r, setR] = useState<RuleDraft>(blankRule);
  const [sample, setSample] = useState("email: ann@example.org\nmessage: I would like a refund");
  const [result, setResult] = useState<Row | null>(null);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: ["form-rules", api.base, id],
    queryFn: () => api.get<{ items: Row[] }>(`/forms/${id}/rules`),
    enabled: !!id,
  });
  useEffect(() => {
    setR(blankRule);
    setResult(null);
    setError("");
  }, [id]);
  const refresh = () => client.invalidateQueries({ queryKey: ["form-rules", api.base, id] });
  const add = async () => {
    setError("");
    try {
      await api.send(`/forms/${id}/rules`, "POST", {
        name: r.name.trim(),
        field: r.field.trim(),
        op: r.op,
        value: r.value,
        set_priority: r.set_priority,
        add_tags: lines(r.add_tags),
        stop: r.stop,
      });
      setR(blankRule);
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const move = async (i: number, dir: -1 | 1) => {
    const ids = (q.data?.items ?? []).map((x) => s(x.id));
    const j = i + dir;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    await run(() => api.send(`/forms/${id}/rules/order`, "PUT", { ids }), "");
    refresh();
  };
  const test = async () => {
    setError("");
    setResult(null);
    const fields: Record<string, string> = {};
    for (const line of sample.split("\n")) {
      const k = line.indexOf(":");
      if (k > 0) fields[line.slice(0, k).trim()] = line.slice(k + 1).trim();
    }
    try {
      setResult(await api.send<Row>(`/forms/${id}/rules/test`, "POST", { fields }));
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Dialog open={!!form} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto">
        <DialogTitle>{phrase("Routing rules")}</DialogTitle>
        <DialogDescription>
          {phrase(
            "Rules run in order on every kept submission: set a priority, add tags or assign it. Suspected spam is left alone.",
          )}
        </DialogDescription>
        <DataState
          q={q}
          isEmpty={(d) => d.items.length === 0}
          empty={<p className="text-sm text-muted-foreground">{phrase("No rules")}</p>}
        >
          {(d) => (
            <Table
              rows={d.items}
              rowKey={(x) => s(x.id)}
              columns={[
                { key: "name", label: phrase("Name"), render: (x) => <b>{s(x.name)}</b> },
                {
                  key: "op",
                  label: phrase("When"),
                  render: (x) => (
                    <span>
                      {s(x.field) || phrase("any field")} {phrase(s(x.op))}{" "}
                      {s(x.op) !== "exists" && <code>{s(x.value)}</code>}
                    </span>
                  ),
                },
                {
                  key: "set_priority",
                  label: phrase("Then"),
                  render: (x) => (
                    <span className="flex flex-wrap gap-1 text-xs">
                      {s(x.set_priority) && <span>{phrase(s(x.set_priority))}</span>}
                      {((x.add_tags as string[]) ?? []).map((t) => (
                        <span key={t} className="rounded bg-muted px-1.5 py-0.5">
                          {t}
                        </span>
                      ))}
                      {s(x.assign_to_email) && <span>{s(x.assign_to_email)}</span>}
                      {!!x.stop && <span>{phrase("then stop")}</span>}
                    </span>
                  ),
                },
                {
                  key: "enabled",
                  label: phrase("Enabled"),
                  render: (x) => (
                    <input
                      type="checkbox"
                      aria-label={phrase("Enabled")}
                      checked={!!x.enabled}
                      onChange={async (e) => {
                        await run(
                          () =>
                            api.send(`/forms/${id}/rules/${s(x.id)}`, "PUT", {
                              name: s(x.name),
                              field: s(x.field),
                              op: s(x.op),
                              value: s(x.value),
                              set_priority: s(x.set_priority),
                              add_tags: x.add_tags,
                              assign_to: s(x.assign_to),
                              stop: !!x.stop,
                              enabled: e.target.checked,
                            }),
                          "",
                        );
                        refresh();
                      }}
                    />
                  ),
                },
                {
                  key: "id",
                  label: "",
                  render: (x) => {
                    const i = d.items.findIndex((y) => s(y.id) === s(x.id));
                    return (
                      <span className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="outline"
                          aria-label={phrase("Move up")}
                          disabled={i === 0}
                          onClick={() => move(i, -1)}
                        >
                          <ArrowUp size={14} />
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          aria-label={phrase("Move down")}
                          disabled={i === d.items.length - 1}
                          onClick={() => move(i, 1)}
                        >
                          <ArrowDown size={14} />
                        </Button>
                        <ConfirmButton
                          title={phrase("Delete this rule?")}
                          description={phrase("Past submissions keep their labels.")}
                          confirm="Delete"
                          onConfirm={async () => {
                            await run(
                              () => api.send(`/forms/${id}/rules/${s(x.id)}`, "DELETE"),
                              "",
                            );
                            refresh();
                          }}
                        >
                          <Trash2 size={14} />
                        </ConfirmButton>
                      </span>
                    );
                  },
                },
              ]}
            />
          )}
        </DataState>
        <Section title={phrase("Add a rule")}>
          <div className="grid gap-3 sm:grid-cols-4">
            <Field label={phrase("Name")}>
              <input
                className={inputClass}
                value={r.name}
                onChange={(e) => setR({ ...r, name: e.target.value })}
              />
            </Field>
            <Field label={phrase("Field")} hint={phrase("Empty means any field.")}>
              <input
                className={inputClass}
                value={r.field}
                onChange={(e) => setR({ ...r, field: e.target.value })}
              />
            </Field>
            <Field label={phrase("Condition")}>
              <select
                className={selectClass}
                value={r.op}
                onChange={(e) => setR({ ...r, op: e.target.value })}
              >
                {ruleOps.map((x) => (
                  <option key={x} value={x}>
                    {phrase(x)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={phrase("Value")}>
              <input
                className={inputClass}
                value={r.value}
                disabled={r.op === "exists"}
                onChange={(e) => setR({ ...r, value: e.target.value })}
              />
            </Field>
            <Field label={phrase("Set priority")}>
              <select
                className={selectClass}
                value={r.set_priority}
                onChange={(e) => setR({ ...r, set_priority: e.target.value })}
              >
                <option value="">{phrase("Leave as is")}</option>
                {priorities.map((x) => (
                  <option key={x} value={x}>
                    {phrase(x)}
                  </option>
                ))}
              </select>
            </Field>
            <Field
              label={phrase("Add tags")}
              hint={phrase("Separate with commas, up to 10.")}
              className="sm:col-span-2"
            >
              <input
                className={inputClass}
                value={r.add_tags}
                onChange={(e) => setR({ ...r, add_tags: e.target.value })}
              />
            </Field>
            <label className="flex items-center gap-2 self-end text-sm">
              <input
                type="checkbox"
                checked={r.stop}
                onChange={(e) => setR({ ...r, stop: e.target.checked })}
              />
              {phrase("Skip the later rules")}
            </label>
          </div>
          {error && (
            <p role="alert" className="mt-2 text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="mt-3 flex justify-end">
            <Button onClick={add} disabled={!r.name.trim()}>
              {phrase("Add rule")}
            </Button>
          </div>
        </Section>
        <Section title={phrase("Try the rules")}>
          <Field
            label={phrase("A sample submission")}
            hint={phrase("One field per line, as name: value.")}
          >
            <textarea
              className={textareaClass}
              rows={3}
              value={sample}
              onChange={(e) => setSample(e.target.value)}
            />
          </Field>
          <div className="mt-2 flex items-center gap-3">
            <Button variant="outline" onClick={test}>
              {phrase("Try it")}
            </Button>
            {result && (
              <p className="text-sm" role="status">
                {phrase("Priority")}: {phrase(s(result.priority))} · {phrase("Tags")}:{" "}
                {((result.tags as string[]) ?? []).join(", ") || "-"} · {phrase("Matched")}:{" "}
                {((result.matched as string[]) ?? []).join(", ") || "-"}
              </p>
            )}
          </div>
        </Section>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- filters

const kinds = ["keyword", "phrase", "regex", "email", "domain", "ip"];

export function FormFilters() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useFormsEnv();
  const client = useQueryClient();
  const [f, setF] = useState({ kind: "keyword", pattern: "", action: "quarantine" });
  const [test, setTest] = useState<Row | null>(null);
  const [error, setError] = useState("");
  const list = useQuery({
    queryKey: key("form-filters"),
    queryFn: () => api.get<{ items: Row[] }>("/forms/filters"),
  });
  const refresh = () => client.invalidateQueries({ queryKey: key("form-filters") });
  const dryRun = async () => {
    setError("");
    setTest(null);
    try {
      setTest(await api.send<Row>("/forms/filters/test", "POST", f));
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const add = async () => {
    setError("");
    try {
      await api.send("/forms/filters", "POST", f);
      setF({ ...f, pattern: "" });
      setTest(null);
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <>
      <PageHeader
        title={phrase("Spam filters")}
        description={phrase(
          "Your own rules, applied to every form on top of the built-in checks. Try a rule against past submissions before you save it.",
        )}
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} />
      </div>
      <Section title={phrase("Add a rule")} className="mb-4">
        <div className="grid gap-3 sm:grid-cols-4">
          <Field label={phrase("Match")}>
            <select
              className={selectClass}
              value={f.kind}
              onChange={(e) => setF({ ...f, kind: e.target.value })}
            >
              {kinds.map((x) => (
                <option key={x} value={x}>
                  {phrase(x)}
                </option>
              ))}
            </select>
          </Field>
          <Field label={phrase("Pattern")} className="sm:col-span-2">
            <input
              className={inputClass}
              value={f.pattern}
              onChange={(e) => setF({ ...f, pattern: e.target.value })}
            />
          </Field>
          <Field label={phrase("Then")}>
            <select
              className={selectClass}
              value={f.action}
              onChange={(e) => setF({ ...f, action: e.target.value })}
            >
              {["flag", "quarantine", "reject"].map((x) => (
                <option key={x} value={x}>
                  {phrase(x)}
                </option>
              ))}
            </select>
          </Field>
        </div>
        {error && (
          <p role="alert" className="mt-2 text-sm text-destructive">
            {error}
          </p>
        )}
        {test && (
          <p className="mt-2 text-sm" role="status">
            {phrase("Checked")} {n(test.checked)} · {phrase("would match")} {n(test.would_match)}
          </p>
        )}
        <div className="mt-3 flex gap-2">
          <Button variant="outline" onClick={dryRun} disabled={!f.pattern.trim()}>
            {phrase("Try it on past submissions")}
          </Button>
          <Button onClick={add} disabled={!f.pattern.trim()}>
            {phrase("Add rule")}
          </Button>
        </div>
      </Section>
      <DataState
        q={list}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No rules")}
              description={phrase("The built-in checks still run on every submission.")}
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              { key: "kind", label: phrase("Match"), render: (r) => phrase(s(r.kind)) },
              {
                key: "pattern",
                label: phrase("Pattern"),
                render: (r) => <code>{s(r.pattern)}</code>,
              },
              { key: "action", label: phrase("Then"), render: (r) => phrase(s(r.action)) },
              {
                key: "enabled",
                label: phrase("Enabled"),
                render: (r) => (
                  <input
                    type="checkbox"
                    aria-label={phrase("Enabled")}
                    checked={!!r.enabled}
                    onChange={async (e) => {
                      await run(
                        () =>
                          api.send(`/forms/filters/${s(r.id)}`, "PATCH", {
                            enabled: e.target.checked,
                          }),
                        "",
                      );
                      refresh();
                    }}
                  />
                ),
              },
              {
                key: "id",
                label: "",
                render: (r) => (
                  <ConfirmButton
                    title={phrase("Delete this rule?")}
                    description={phrase("Past submissions keep their result.")}
                    confirm="Delete"
                    onConfirm={async () => {
                      await run(() => api.send(`/forms/filters/${s(r.id)}`, "DELETE"), "");
                      refresh();
                    }}
                  >
                    <Trash2 size={14} />
                  </ConfirmButton>
                ),
              },
            ]}
          />
        )}
      </DataState>
    </>
  );
}
