import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  Ban,
  Forward,
  MailOpen,
  Paperclip,
  Reply,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import { useCompose } from "../Layout";
import {
  ConfirmButton,
  CopyButton,
  DataState,
  Empty,
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
import { cn } from "@/lib/utils";

// ---------------------------------------------------------------- inbox

type InboxMsg = Row & { id: string };

export function Inbox() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const [mailbox, setMailbox] = useParam("mailbox");
  const [unread, setUnread] = useParam("unread");
  const [archived, setArchived] = useParam("archived");
  const [q, setQ] = useParam("q");
  const [open, setOpen] = useParam("m");
  const [search, setSearch] = useState(q);
  const offset = useOffset();
  const mailboxes = useQuery({
    queryKey: key("mailboxes"),
    queryFn: () => api.get<Page>("/mailboxes?limit=100"),
  });
  const counts = useQuery({
    queryKey: key("unread"),
    queryFn: () => api.get<{ total: number; mailboxes: Record<string, number> }>("/inbox/unread"),
  });
  const params = new URLSearchParams({ limit: "50", offset: String(offset) });
  if (mailbox) params.set("mailbox", mailbox);
  if (unread) params.set("unread", "1");
  if (archived) params.set("archived", "1");
  if (q) params.set("q", q);
  const list = useQuery({
    queryKey: key("inbox", params.toString()),
    queryFn: () => api.get<Page<InboxMsg>>(`/inbox?${params}`),
    refetchInterval: 20_000, // new mail appears without a reload
  });
  const noMailbox = mailboxes.isSuccess && mailboxes.data.items.length === 0;
  return (
    <>
      <PageHeader
        title={phrase("Inbox")}
        description={phrase("Mail received by this project's mailboxes.")}
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <Field label={phrase("Mailbox")}>
          <select
            className={selectClass}
            value={mailbox}
            onChange={(e) => setMailbox(e.target.value)}
          >
            <option value="">
              {phrase("All mailboxes")} {counts.data?.total ? `(${counts.data.total})` : ""}
            </option>
            {mailboxes.data?.items.map((m) => (
              <option key={s(m.id)} value={s(m.id)}>
                {s(m.address)}{" "}
                {counts.data?.mailboxes[s(m.id)] ? `(${counts.data.mailboxes[s(m.id)]})` : ""}
              </option>
            ))}
          </select>
        </Field>
        <form
          className="flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            setQ(search.trim());
          }}
        >
          <Field label={phrase("Search sender, subject or body")}>
            <input
              className={inputClass}
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onBlur={() => setQ(search.trim())}
            />
          </Field>
        </form>
        <div
          className="flex gap-1 rounded-lg bg-muted p-1 text-sm"
          role="group"
          aria-label={phrase("Filter")}
        >
          {[
            ["", "", "All"],
            ["1", "", "Unread"],
            ["", "1", "Archived"],
          ].map(([u, a, label]) => (
            <button
              key={label}
              aria-pressed={unread === u && archived === a}
              onClick={() => {
                setUnread(u);
                setArchived(a);
              }}
              className={cn(
                "rounded-md px-3 py-1",
                unread === u && archived === a ? "bg-background shadow" : "text-muted-foreground",
              )}
            >
              {phrase(label)}
              {label === "Unread" && counts.data?.total ? ` (${counts.data.total})` : ""}
            </button>
          ))}
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <div className={cn("min-w-0", open && "hidden lg:block")}>
          <DataState
            q={list}
            isEmpty={(d) => d.items.length === 0}
            empty={
              <div className="rounded-xl border bg-card">
                <Empty
                  title={phrase("No messages")}
                  description={
                    noMailbox
                      ? phrase("This project has no mailbox yet. Create one to receive mail.")
                      : phrase("Nothing matches these filters.")
                  }
                  action={
                    noMailbox ? (
                      <Button asChild>
                        <Link to={to("mailboxes")}>{phrase("Create a mailbox")}</Link>
                      </Button>
                    ) : undefined
                  }
                />
              </div>
            }
          >
            {(d) => (
              <>
                <ul className="divide-y overflow-hidden rounded-xl border bg-card">
                  {d.items.map((m) => {
                    const isUnread = !m.read_at;
                    return (
                      <li key={m.id}>
                        <button
                          onClick={() => setOpen(m.id)}
                          aria-current={open === m.id}
                          className={cn(
                            "block w-full px-4 py-3 text-left hover:bg-muted/60 focus-visible:bg-muted focus-visible:outline-none",
                            open === m.id && "bg-primary/10",
                          )}
                        >
                          <div className="flex items-center gap-2">
                            {isUnread && (
                              <span
                                className="h-2 w-2 shrink-0 rounded-full bg-primary"
                                aria-label={phrase("unread")}
                              />
                            )}
                            <span
                              className={cn(
                                "min-w-0 flex-1 truncate text-sm",
                                isUnread && "font-semibold",
                              )}
                            >
                              {s(m.header_from) || s(m.envelope_from)}
                            </span>
                            {Number(m.attachment_count) > 0 && (
                              <Paperclip size={13} aria-label={phrase("has attachments")} />
                            )}
                            <span className="shrink-0 text-xs text-muted-foreground">
                              <Time value={m.received_at} relative />
                            </span>
                          </div>
                          <p
                            className={cn(
                              "truncate text-sm",
                              isUnread ? "font-medium" : "text-muted-foreground",
                            )}
                          >
                            {s(m.subject) || phrase("(no subject)")}
                          </p>
                          <p className="truncate text-xs text-muted-foreground">{s(m.snippet)}</p>
                        </button>
                      </li>
                    );
                  })}
                </ul>
                <Pager page={d} />
              </>
            )}
          </DataState>
        </div>
        <div className={cn("min-w-0", !open && "hidden lg:block")}>
          {open ? (
            <Reader id={open} onClose={() => setOpen("")} />
          ) : (
            <div className="hidden h-full min-h-64 place-items-center rounded-xl border border-dashed text-sm text-muted-foreground lg:grid">
              {phrase("Choose a message to read it.")}
            </div>
          )}
        </div>
      </div>
    </>
  );
}

function Reader({ id, onClose }: { id: string; onClose: () => void }) {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const compose = useCompose();
  const [view, setView] = useState<"text" | "html">("text");
  const [headers, setHeaders] = useState(false);
  const q = useQuery({
    queryKey: key("inbox-message", id),
    queryFn: async () => {
      const m = await api.get<Row>(`/inbox/${id}`);
      client.invalidateQueries({ queryKey: key("unread") });
      client.invalidateQueries({ queryKey: key("inbox") });
      return m;
    },
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("inbox") });
    client.invalidateQueries({ queryKey: key("unread") });
    client.invalidateQueries({ queryKey: key("inbox-message", id) });
  };
  return (
    <DataState q={q}>
      {(m) => {
        const atts = (m.attachments as Row[]) ?? [];
        const from = s(m.envelope_from);
        const quote = `<p></p><blockquote>${s(m.html) || s(m.text).replace(/\n/g, "<br>")}</blockquote>`;
        return (
          <article className="rounded-xl border bg-card">
            <div className="flex flex-wrap items-center gap-1 border-b p-2">
              <Button variant="ghost" size="sm" className="lg:hidden" onClick={onClose}>
                <ArrowLeft size={15} /> {phrase("Back")}
              </Button>
              <Button
                size="sm"
                onClick={() =>
                  compose.open({
                    to: from,
                    subject: s(m.subject).startsWith("Re:") ? s(m.subject) : `Re: ${s(m.subject)}`,
                    from: s(m.reply_sender) || undefined,
                    inReplyTo: id,
                    html: quote,
                  })
                }
              >
                <Reply size={15} /> {phrase("Reply")}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  compose.open({
                    subject: `Fwd: ${s(m.subject)}`,
                    from: s(m.reply_sender) || undefined,
                    html: quote,
                  })
                }
              >
                <Forward size={15} /> {phrase("Forward")}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  await run(
                    () => api.send(`/inbox/${id}`, "PATCH", { read: false }),
                    phrase("Marked as unread."),
                  );
                  refresh();
                  onClose();
                }}
              >
                <MailOpen size={15} /> {phrase("Mark unread")}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  await run(
                    () => api.send(`/inbox/${id}`, "PATCH", { archived: !m.archived_at }),
                    m.archived_at ? phrase("Moved to Inbox.") : phrase("Archived."),
                  );
                  refresh();
                  onClose();
                }}
              >
                {m.archived_at ? <ArchiveRestore size={15} /> : <Archive size={15} />}{" "}
                {m.archived_at ? phrase("Unarchive") : phrase("Archive")}
              </Button>
              <ConfirmButton
                variant="ghost"
                title={phrase("Block this sender?")}
                description={`${phrase("Mail from")} ${from} ${phrase("will no longer be stored in this environment, and their messages here are archived.")}`}
                confirm="Block sender"
                disabled={!!m.sender_blocked}
                onConfirm={async () => {
                  await run(
                    () => api.send(`/inbox/${id}/block`, "POST"),
                    phrase("Sender blocked."),
                  );
                  refresh();
                }}
              >
                <Ban size={15} /> {m.sender_blocked ? phrase("Blocked") : phrase("Block sender")}
              </ConfirmButton>
              <ConfirmButton
                variant="ghost"
                title={phrase("Delete this message?")}
                description={phrase("The message and its attachments are deleted permanently.")}
                confirm="Delete"
                onConfirm={async () => {
                  if (
                    await run(() => api.send(`/inbox/${id}`, "DELETE"), phrase("Message deleted."))
                  ) {
                    refresh();
                    onClose();
                  }
                }}
              >
                <Trash2 size={15} /> {phrase("Delete")}
              </ConfirmButton>
            </div>
            <div className="space-y-1 border-b p-4">
              <h2 className="text-lg font-semibold">{s(m.subject) || phrase("(no subject)")}</h2>
              <p className="text-sm">
                <span className="font-medium">{s(m.header_from)}</span>{" "}
                <span className="text-muted-foreground">&lt;{from}&gt;</span>
              </p>
              <p className="text-xs text-muted-foreground">
                {phrase("To")} {s(m.envelope_to)}
                {m.mailbox_id ? (
                  <>
                    {" · "}
                    <Link className="text-primary" to={to(`inbox?mailbox=${s(m.mailbox_id)}`)}>
                      {s(m.mailbox)}
                    </Link>
                  </>
                ) : null}
                {" · "}
                <Time value={m.received_at} />
                {m.spam_score != null && ` · ${phrase("spam score")} ${s(m.spam_score)}`}
              </p>
              {m.in_reply_to ? (
                <p className="text-xs">
                  {phrase("Reply to")}{" "}
                  <Link className="text-primary underline" to={to(`messages/${s(m.in_reply_to)}`)}>
                    {phrase("our message")}
                  </Link>
                </p>
              ) : null}
              {((m.automation_runs as Row[]) ?? []).map((r, i) => (
                <p key={i} className="text-xs text-muted-foreground">
                  {phrase("Automation")}{" "}
                  <Link className="text-primary" to={to(`automations?rule=${s(r.rule_id)}`)}>
                    {s(r.name)}
                  </Link>
                  : {s(r.result)} — {s(r.detail)}
                </p>
              ))}
            </div>
            <div className="flex gap-2 border-b px-4 py-2 text-sm" role="tablist">
              {(["text", "html"] as const).map((v) => (
                <button
                  key={v}
                  role="tab"
                  aria-selected={view === v}
                  onClick={() => setView(v)}
                  className={cn(
                    "rounded px-2 py-1",
                    view === v ? "bg-muted font-medium" : "text-muted-foreground",
                  )}
                >
                  {v === "text" ? phrase("Text") : "HTML"}
                </button>
              ))}
              <button className="ml-auto text-xs text-primary" onClick={() => setHeaders(!headers)}>
                {headers ? phrase("Hide headers") : phrase("Show headers")}
              </button>
            </div>
            {headers && (
              <pre className="max-h-60 overflow-auto border-b bg-muted/30 p-3 text-xs">
                {JSON.stringify(m.headers, null, 2)}
              </pre>
            )}
            <div className="p-4">
              {m.bodies_purged_at ? (
                <p className="text-sm text-muted-foreground">
                  {phrase("The body was removed by the retention policy.")}
                </p>
              ) : view === "text" ? (
                <pre className="whitespace-pre-wrap break-words font-sans text-sm">
                  {s(m.text) || phrase("(no text part)")}
                </pre>
              ) : s(m.html) ? (
                <iframe
                  title={phrase("Message HTML (sandboxed)")}
                  sandbox=""
                  referrerPolicy="no-referrer"
                  className="h-[28rem] w-full rounded border bg-white"
                  src={`/admin/v1${api.base}/inbox/${id}/html`}
                />
              ) : (
                <p className="text-sm text-muted-foreground">{phrase("(no HTML part)")}</p>
              )}
            </div>
            {atts.length > 0 && (
              <ul className="flex flex-wrap gap-2 border-t p-4">
                {atts.map((a) => (
                  <li key={s(a.id)}>
                    <a
                      className="inline-flex items-center gap-1 rounded-full border px-3 py-1 text-xs hover:bg-muted"
                      href={`/admin/v1${api.base}/inbox/${id}/attachments/${s(a.id)}`}
                    >
                      <Paperclip size={12} /> {s(a.filename)} ({Math.ceil(Number(a.size) / 1024)}{" "}
                      KB)
                    </a>
                  </li>
                ))}
              </ul>
            )}
          </article>
        );
      }}
    </DataState>
  );
}

// ---------------------------------------------------------------- message lists

const STATES = [
  "queued",
  "scheduled",
  "submitting",
  "submitted",
  "retry_wait",
  "failed",
  "unknown",
  "expired",
  "captured",
  "cancelled",
];
const SOURCES = [
  ["ui", "Workspace"],
  ["api", "API"],
  ["bulk", "Bulk"],
  ["automation", "Automation"],
  ["forward", "Forward"],
  ["test", "Template test"],
];

function MessageList({ preset, extended }: { preset?: { state?: string }; extended?: boolean }) {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [state, setState] = useParam("state", preset?.state ?? "");
  const [source, setSource] = useParam("source");
  const [sender, setSender] = useParam("sender");
  const [template] = useParam("template");
  const [apiKey] = useParam("api_key");
  const [campaign] = useParam("campaign");
  const [recipient, setRecipient] = useParam("recipient");
  const [from, setFrom] = useParam("from");
  const [until, setUntil] = useParam("to");
  const [rcpt, setRcpt] = useState(recipient);
  const offset = useOffset();
  const params = new URLSearchParams({ limit: "50", offset: String(offset) });
  for (const [k, v] of Object.entries({
    state,
    source,
    sender,
    template,
    api_key: apiKey,
    campaign,
    recipient,
  }))
    if (v) params.set(k, v);
  if (from) params.set("from", new Date(from).toISOString());
  if (until) params.set("to", new Date(until + "T23:59:59").toISOString());
  const q = useQuery({
    queryKey: key("messages", params.toString()),
    queryFn: () => api.get<Page>(`/messages?${params}`),
    refetchInterval: 15_000,
  });
  const senders = useQuery({
    queryKey: key("senders"),
    queryFn: () => api.get<Page>("/senders?limit=100"),
  });
  const scheduled = preset?.state === "scheduled";
  const active = [
    template && `template ${template}`,
    apiKey && `API key ${apiKey}`,
    campaign && "one announcement",
  ].filter(Boolean);
  return (
    <>
      <div className="mb-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        {!preset?.state && (
          <Field label={phrase("State")}>
            <select
              className={selectClass}
              value={state}
              onChange={(e) => setState(e.target.value)}
            >
              <option value="">{phrase("All states")}</option>
              {STATES.map((x) => (
                <option key={x} value={x}>
                  {x.replace("_", " ")}
                </option>
              ))}
            </select>
          </Field>
        )}
        <Field label={phrase("Source")}>
          <select
            className={selectClass}
            value={source}
            onChange={(e) => setSource(e.target.value)}
          >
            <option value="">{phrase("All sources")}</option>
            {SOURCES.map(([v, l]) => (
              <option key={v} value={v}>
                {phrase(l)}
              </option>
            ))}
          </select>
        </Field>
        <Field label={phrase("Sender")}>
          <select
            className={selectClass}
            value={sender}
            onChange={(e) => setSender(e.target.value)}
          >
            <option value="">{phrase("All senders")}</option>
            {senders.data?.items.map((x) => (
              <option key={s(x.id)} value={s(x.email)}>
                {s(x.email)}
              </option>
            ))}
          </select>
        </Field>
        <Field label={phrase("From date")}>
          <input
            type="date"
            className={inputClass}
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </Field>
        <Field label={phrase("To date")}>
          <input
            type="date"
            className={inputClass}
            value={until}
            onChange={(e) => setUntil(e.target.value)}
          />
        </Field>
        {extended && (
          <form
            className="sm:col-span-2"
            onSubmit={(e) => {
              e.preventDefault();
              setRecipient(rcpt.trim());
            }}
          >
            <Field
              label={phrase("Recipient (full address)")}
              hint={phrase("Matched exactly; addresses are stored only as a keyed hash.")}
            >
              <input
                className={inputClass}
                type="search"
                value={rcpt}
                onChange={(e) => setRcpt(e.target.value)}
                onBlur={() => setRecipient(rcpt.trim())}
              />
            </Field>
          </form>
        )}
      </div>
      {active.length > 0 && (
        <p className="mb-3 text-sm">
          {phrase("Filtered by")} {active.join(", ")} ·{" "}
          <Link className="text-primary" to="?">
            {phrase("clear")}
          </Link>
        </p>
      )}
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={scheduled ? phrase("Nothing is scheduled") : phrase("No messages match")}
              description={phrase(
                "Write a message with Compose, or send from your application with an API key.",
              )}
              action={
                <Button asChild variant="outline">
                  <Link to={to("api-docs")}>{phrase("API quick start")}</Link>
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
              onRow={(r) => navigate(to(`messages/${s(r.id)}`))}
              columns={[
                {
                  key: "recipient",
                  label: phrase("Recipient"),
                  render: (r) => s(r.recipient) || "—",
                },
                {
                  key: "subject",
                  label: phrase("Subject or template"),
                  render: (r) => (
                    <Link to={to(`messages/${s(r.id)}`)} className="hover:underline">
                      {s(r.subject) || (
                        <span className="text-muted-foreground">{s(r.template)}</span>
                      )}
                    </Link>
                  ),
                },
                { key: "sender", label: phrase("Sender") },
                {
                  key: "state",
                  label: phrase("State"),
                  render: (r) => <StateBadge state={s(r.state)} />,
                },
                { key: "source", label: phrase("Source"), className: "hidden md:table-cell" },
                {
                  key: "time",
                  label: scheduled ? phrase("Send at") : phrase("Created"),
                  render: (r) => <Time value={scheduled ? r.send_at : r.created_at} />,
                },
                ...(scheduled
                  ? [
                      {
                        key: "actions",
                        label: <span className="sr-only">{phrase("Actions")}</span>,
                        render: (r: Row) => (
                          <span className="flex gap-1">
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={async () => {
                                await run(
                                  () => api.send(`/messages/${s(r.id)}/send-now`, "POST"),
                                  phrase("Queued now."),
                                );
                                client.invalidateQueries({ queryKey: key("messages") });
                              }}
                            >
                              {phrase("Send now")}
                            </Button>
                            <ConfirmButton
                              title={phrase("Cancel this scheduled message?")}
                              description={`${phrase("It will not be sent to")} ${s(r.recipient)}.`}
                              confirm="Cancel message"
                              onConfirm={async () => {
                                await run(
                                  () => api.send(`/messages/${s(r.id)}/cancel`, "POST"),
                                  phrase("Cancelled."),
                                );
                                client.invalidateQueries({ queryKey: key("messages") });
                              }}
                            >
                              {phrase("Cancel")}
                            </ConfirmButton>
                          </span>
                        ),
                      },
                    ]
                  : []),
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
    </>
  );
}

export function Sent() {
  const phrase = usePhrase();
  const compose = useCompose();
  return (
    <>
      <PageHeader
        title={phrase("Sent")}
        description={phrase(
          "Messages sent from this project. States are the backend's own: “submitted” means the mail server accepted it.",
        )}
        action={<Button onClick={() => compose.open()}>{phrase("Compose")}</Button>}
      />
      <MessageList />
    </>
  );
}

export function Scheduled() {
  const phrase = usePhrase();
  return (
    <>
      <PageHeader
        title={phrase("Scheduled")}
        description={phrase("Messages waiting for their send time. Send one now or cancel it.")}
      />
      <MessageList preset={{ state: "scheduled" }} />
    </>
  );
}

export function MessageLog() {
  const phrase = usePhrase();
  return (
    <>
      <PageHeader
        title={phrase("Message log")}
        description={phrase(
          "Every message of this project and environment, from every source. Metadata only.",
        )}
      />
      <MessageList extended />
    </>
  );
}

export function UnknownQueue() {
  const phrase = usePhrase();
  return (
    <>
      <PageHeader
        title={phrase("Unknown queue")}
        description={phrase(
          "The mail server may or may not have accepted these. They are never retried automatically. Check the mail server log, then record the outcome on each message.",
        )}
      />
      <MessageList preset={{ state: "unknown" }} />
    </>
  );
}

// ---------------------------------------------------------------- message detail

export function MessageDetail() {
  const phrase = usePhrase();
  const { id = "" } = useParams();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const navigate = useNavigate();
  const compose = useCompose();
  const [as, setAs] = useState("submitted");
  const [note, setNote] = useState("");
  const q = useQuery({
    queryKey: key("message", id),
    queryFn: () => api.get<Row>(`/messages/${id}`),
  });
  const copy = useQuery({
    queryKey: key("message-copy", id),
    queryFn: () => api.get<Row>(`/messages/${id}/copy`),
    enabled: q.data?.has_copy === true,
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("message", id) });
    client.invalidateQueries({ queryKey: key("messages") });
  };
  return (
    <DataState q={q}>
      {(m) => {
        const state = s(m.state);
        const c = copy.data;
        const facts: [string, React.ReactNode][] = [
          [phrase("State"), <StateBadge key="s" state={state} />],
          [phrase("Recipient"), s(m.recipient) || "—"],
          [
            phrase("Sender"),
            m.sender_id ? (
              <Link key="snd" className="text-primary" to={to("senders")}>
                {s(m.sender)}
              </Link>
            ) : (
              s(m.sender) || "—"
            ),
          ],
          [
            phrase("Domain"),
            m.domain_id ? (
              <Link key="d" className="text-primary" to={to(`domains/${s(m.domain_id)}`)}>
                {s(m.sender).split("@")[1]}
              </Link>
            ) : (
              "—"
            ),
          ],
          [
            phrase("Template"),
            m.template_id && s(m.template) !== "raw" ? (
              <Link key="t" className="text-primary" to={to(`templates?id=${s(m.template_id)}`)}>
                {s(m.template)} v{s(m.template_version)}
              </Link>
            ) : (
              phrase("Written message")
            ),
          ],
          [phrase("Source"), s(m.source)],
          [
            phrase("API key"),
            m.api_key_prefix ? (
              <Link
                key="k"
                className="text-primary"
                to={to(`messages?api_key=${s(m.api_key_prefix)}`)}
              >
                {s(m.api_key_prefix)}
              </Link>
            ) : (
              "—"
            ),
          ],
          [
            phrase("Announcement"),
            m.campaign_id ? (
              <Link key="c" className="text-primary" to={to(`bulk/${s(m.campaign_id)}`)}>
                {s(m.campaign_name)}
              </Link>
            ) : (
              "—"
            ),
          ],
          [phrase("Priority"), s(m.priority)],
          [phrase("Attempts"), s(m.attempts)],
          [phrase("Created"), <Time key="c" value={m.created_at} />],
          [phrase("Send at"), <Time key="sa" value={m.send_at} />],
          [phrase("Submitted"), <Time key="su" value={m.submitted_at} />],
          [phrase("Expires"), <Time key="e" value={m.expires_at} />],
          [phrase("Mail server reply"), s(m.smtp_reply) || "—"],
          [
            phrase("Suppression"),
            m.suppression_id ? (
              <Link key="sp" className="text-primary" to={to("suppressions")}>
                {phrase("recipient is suppressed")}
              </Link>
            ) : (
              phrase("none")
            ),
          ],
        ];
        return (
          <>
            <PageHeader
              back={{
                to: to(state === "scheduled" ? "scheduled" : "sent"),
                label: phrase(state === "scheduled" ? "Scheduled" : "Sent"),
              }}
              title={s(m.subject) || s(m.template) || phrase("Message")}
              description={
                <span className="inline-flex items-center gap-2">
                  <code className="text-xs">{id}</code> <CopyButton text={id} label="message ID" />
                </span>
              }
              action={
                <>
                  {state === "scheduled" && (
                    <Button
                      onClick={async () => {
                        await run(
                          () => api.send(`/messages/${id}/send-now`, "POST"),
                          phrase("Queued now."),
                        );
                        refresh();
                      }}
                    >
                      {phrase("Send now")}
                    </Button>
                  )}
                  {(state === "queued" || state === "scheduled") && (
                    <ConfirmButton
                      size="default"
                      title={phrase("Cancel this message?")}
                      description={phrase("It will not be sent. This cannot be undone.")}
                      confirm="Cancel message"
                      onConfirm={async () => {
                        await run(
                          () => api.send(`/messages/${id}/cancel`, "POST"),
                          phrase("Cancelled."),
                        );
                        refresh();
                      }}
                    >
                      {phrase("Cancel")}
                    </ConfirmButton>
                  )}
                  {(state === "failed" || state === "expired") && m.has_copy === true && (
                    <ConfirmButton
                      size="default"
                      destructive={false}
                      variant="default"
                      title={phrase("Send this message again?")}
                      description={phrase(
                        "A new message is queued with the same content. The first one ended without being accepted.",
                      )}
                      confirm="Send again"
                      onConfirm={async () => {
                        const r = await run(
                          () => api.send<{ id: string }>(`/messages/${id}/retry`, "POST"),
                          phrase("Queued again."),
                        );
                        if (r) navigate(to(`messages/${r.id}`));
                      }}
                    >
                      {phrase("Retry")}
                    </ConfirmButton>
                  )}
                  {c && (
                    <Button
                      variant="outline"
                      onClick={() =>
                        compose.open({
                          to: s(c.to),
                          subject: s(c.subject),
                          html: s(c.html),
                          from: s(c.from),
                          template: s(c.template),
                        })
                      }
                    >
                      {phrase("Send again")}
                    </Button>
                  )}
                  {m.has_capture === true && (
                    <Button asChild variant="outline">
                      <Link to={to(`sandbox/${id}`)}>{phrase("View capture")}</Link>
                    </Button>
                  )}
                </>
              }
            />
            <div className="grid gap-4 lg:grid-cols-3">
              <Section className="lg:col-span-2" title={phrase("Details")}>
                <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
                  {facts.map(([k, v]) => (
                    <div key={k}>
                      <dt className="text-xs text-muted-foreground">{k}</dt>
                      <dd className="truncate text-sm">{v}</dd>
                    </div>
                  ))}
                </dl>
              </Section>
              <Section title={phrase("Timeline")}>
                <ol className="space-y-2 text-sm">
                  {((m.events as Row[]) ?? []).map((e, i) => (
                    <li key={i} className="border-l-2 border-primary/40 pl-3">
                      <span className="font-medium">{s(e.type)}</span>{" "}
                      {e.reason ? (
                        <span className="text-muted-foreground">· {s(e.reason)}</span>
                      ) : null}
                      <span className="block text-xs text-muted-foreground">
                        <Time value={e.at} />
                      </span>
                    </li>
                  ))}
                </ol>
              </Section>
            </div>
            {state === "unknown" && (
              <Section className="mt-4" title={phrase("Record the real outcome")}>
                <p className="mb-3 text-sm text-muted-foreground">
                  {phrase(
                    "Find the message in the mail server log first. This records what happened; it does not send again.",
                  )}
                </p>
                <div className="flex flex-wrap items-end gap-3">
                  <Field label={phrase("Outcome")}>
                    <select
                      className={selectClass}
                      value={as}
                      onChange={(e) => setAs(e.target.value)}
                    >
                      <option value="submitted">
                        {phrase("Accepted by the mail server (submitted)")}
                      </option>
                      <option value="failed">{phrase("Not accepted (failed)")}</option>
                    </select>
                  </Field>
                  <Field label={phrase("Evidence")} className="flex-1">
                    <input
                      className={inputClass}
                      value={note}
                      onChange={(e) => setNote(e.target.value)}
                      placeholder={phrase("Mail server log reference")}
                    />
                  </Field>
                  <Button
                    disabled={!note.trim()}
                    onClick={async () => {
                      await run(
                        () => api.send(`/messages/${id}/resolve`, "POST", { as, note }),
                        phrase("Outcome recorded."),
                      );
                      refresh();
                    }}
                  >
                    {phrase("Record outcome")}
                  </Button>
                </div>
              </Section>
            )}
            <div className="mt-4 grid gap-4 lg:grid-cols-2">
              <Section title={phrase("Bounces and complaints")}>
                {((m.bounces as Row[]) ?? []).length === 0 ? (
                  <p className="text-sm text-muted-foreground">{phrase("None recorded.")}</p>
                ) : (
                  <ul className="space-y-2 text-sm">
                    {(m.bounces as Row[]).map((b) => (
                      <li key={s(b.id)}>
                        <StateBadge
                          state={s(b.type) === "hard" ? "failed" : "pending"}
                          label={s(b.type)}
                        />{" "}
                        {s(b.status_code)} — {s(b.diagnostic)}{" "}
                        <span className="text-xs text-muted-foreground">
                          <Time value={b.received_at} />
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </Section>
              <Section title={phrase("Webhook deliveries")}>
                {((m.webhook_deliveries as Row[]) ?? []).length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    {phrase("No webhook events for this message.")}
                  </p>
                ) : (
                  <ul className="space-y-2 text-sm">
                    {(m.webhook_deliveries as Row[]).map((d) => (
                      <li key={s(d.id)} className="flex flex-wrap items-center gap-2">
                        <StateBadge
                          state={
                            s(d.state) === "succeeded"
                              ? "submitted"
                              : s(d.state) === "dead"
                                ? "failed"
                                : "pending"
                          }
                          label={s(d.state)}
                        />
                        {s(d.event_type)}
                        <Link className="text-primary" to={to(`webhooks/${s(d.endpoint_id)}`)}>
                          {s(d.url)}
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </Section>
            </div>
            {(((m.replies as Row[]) ?? []).length > 0 || m.reply_to_inbound_id) && (
              <Section className="mt-4" title={phrase("Thread")}>
                <ul className="space-y-2 text-sm">
                  {m.reply_to_inbound_id ? (
                    <li>
                      {phrase("This message answers")}{" "}
                      <Link className="text-primary" to={to(`inbox?m=${s(m.reply_to_inbound_id)}`)}>
                        {phrase("an inbound message")}
                      </Link>
                    </li>
                  ) : null}
                  {((m.replies as Row[]) ?? []).map((r) => (
                    <li key={s(r.id)}>
                      {phrase("Reply from")} {s(r.header_from)}:{" "}
                      <Link className="text-primary" to={to(`inbox?m=${s(r.id)}`)}>
                        {s(r.subject)}
                      </Link>{" "}
                      <Time value={r.received_at} />
                    </li>
                  ))}
                </ul>
              </Section>
            )}
            {c && (
              <Section className="mt-4" title={phrase("What was written")}>
                <p className="text-sm">
                  {phrase("To")}: {s(c.to)}
                  {Array.isArray(c.cc) && c.cc.length
                    ? ` · Cc: ${(c.cc as string[]).join(", ")}`
                    : ""}
                </p>
                <pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap rounded bg-muted/40 p-3 font-sans text-sm">
                  {s(c.text) || s(c.template)}
                </pre>
              </Section>
            )}
          </>
        );
      }}
    </DataState>
  );
}

// ---------------------------------------------------------------- sandbox

export function Sandbox() {
  const phrase = usePhrase();
  const { api, to, env } = useWorkspace();
  const key = useWsKey();
  const navigate = useNavigate();
  const compose = useCompose();
  const offset = useOffset();
  const q = useQuery({
    queryKey: key("sandbox", offset),
    queryFn: () => api.get<Page>(`/sandbox?limit=50&offset=${offset}`),
    refetchInterval: 15_000,
  });
  return (
    <>
      <PageHeader
        title={phrase("Sandbox")}
        description={phrase(
          "Mail sent in the Sandbox environment is captured here instead of being delivered. Captures expire after a day.",
        )}
        action={
          env === "sandbox" ? (
            <Button onClick={() => compose.open()}>{phrase("Send a test")}</Button>
          ) : (
            <Button asChild variant="outline">
              <Link to={to("sandbox").replace("/live/", "/sandbox/")}>
                {phrase("Open the Sandbox environment")}
              </Link>
            </Button>
          )
        }
      />
      {env === "live" && (
        <p className="mb-4 rounded-lg border p-3 text-sm">
          {phrase(
            "You are in Live. Live mail is delivered, not captured, so this list is empty here.",
          )}
        </p>
      )}
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No captured mail")}
              description={phrase(
                "Send a message in Sandbox with Compose or with a sandbox API key.",
              )}
            />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              onRow={(r) => navigate(to(`sandbox/${s(r.id)}`))}
              columns={[
                { key: "recipient", label: phrase("Recipient") },
                {
                  key: "subject",
                  label: phrase("Subject or template"),
                  render: (r) => s(r.subject) || s(r.template),
                },
                { key: "sender", label: phrase("Sender") },
                { key: "source", label: phrase("Source") },
                {
                  key: "created_at",
                  label: phrase("Captured"),
                  render: (r) => <Time value={r.created_at} />,
                },
                {
                  key: "expires_at",
                  label: phrase("Expires"),
                  render: (r) => <Time value={r.expires_at} />,
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

export function CaptureDetail() {
  const phrase = usePhrase();
  const { id = "" } = useParams();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const [view, setView] = useState<"text" | "html" | "raw">("html");
  const q = useQuery({
    queryKey: key("capture", id),
    queryFn: () => api.get<Row>(`/sandbox/${id}`),
  });
  return (
    <DataState q={q}>
      {(c) => (
        <>
          <PageHeader
            back={{ to: to("sandbox"), label: phrase("Sandbox") }}
            title={s(c.subject) || phrase("(no subject)")}
            description={`${s(c.from)} → ${s(c.to)}`}
            action={
              <Button asChild variant="outline">
                <Link to={to(`messages/${id}`)}>{phrase("Message details")}</Link>
              </Button>
            }
          />
          <div className="mb-3 flex gap-2 text-sm" role="tablist">
            {(["html", "text", "raw"] as const).map((v) => (
              <button
                key={v}
                role="tab"
                aria-selected={view === v}
                onClick={() => setView(v)}
                className={cn(
                  "rounded px-3 py-1",
                  view === v ? "bg-muted font-medium" : "text-muted-foreground",
                )}
              >
                {v.toUpperCase()}
              </button>
            ))}
          </div>
          <Section>
            {view === "html" ? (
              s(c.html) ? (
                <iframe
                  title={phrase("Captured HTML (sandboxed)")}
                  sandbox=""
                  srcDoc={s(c.html)}
                  className="h-[32rem] w-full rounded border bg-white"
                />
              ) : (
                <p className="text-sm text-muted-foreground">{phrase("(no HTML part)")}</p>
              )
            ) : view === "text" ? (
              <pre className="whitespace-pre-wrap font-sans text-sm">{s(c.text)}</pre>
            ) : (
              <pre className="max-h-[32rem] overflow-auto text-xs">{s(c.raw)}</pre>
            )}
          </Section>
        </>
      )}
    </DataState>
  );
}

// ---------------------------------------------------------------- temporary inboxes

export function TempInboxes() {
  const phrase = usePhrase();
  const { api, to, info } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [ttl, setTtl] = useState("1h");
  const [busy, setBusy] = useState(false);
  const q = useQuery({
    queryKey: key("temp-inboxes"),
    queryFn: () => api.get<Page>("/temp-inboxes?limit=100"),
    refetchInterval: 20_000,
  });
  const create = async () => {
    setBusy(true);
    const r = await run(
      () => api.send<{ address: string }>("/temp-inboxes", "POST", { ttl }),
      phrase("Temporary inbox created."),
    );
    setBusy(false);
    if (r) client.invalidateQueries({ queryKey: key("temp-inboxes") });
  };
  return (
    <>
      <PageHeader
        title={phrase("Temporary inboxes")}
        description={phrase(
          "Disposable addresses for testing sign-up and password-reset flows. The address and its mail are deleted when it expires.",
        )}
        action={
          <div className="flex items-center gap-2">
            <select
              aria-label={phrase("Lifetime")}
              className={selectClass + " w-28"}
              value={ttl}
              onChange={(e) => setTtl(e.target.value)}
            >
              <option value="1h">{phrase("1 hour")}</option>
              <option value="1d">{phrase("1 day")}</option>
              <option value="7d">{phrase("7 days")}</option>
            </select>
            <Button onClick={create} disabled={busy || !info?.inbound_domain}>
              {phrase("Create address")}
            </Button>
          </div>
        }
      />
      {info && !info.inbound_domain && (
        <p className="mb-4 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
          {phrase(
            "Temporary inboxes need an inbound domain. Set INBOUND_DOMAIN on the server and relay it from Mailcow (see the Mail server page).",
          )}
        </p>
      )}
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No temporary inboxes")}
              description={`${phrase("At most")} ${info?.temp_inbox_limit ?? 10} ${phrase("can be active at once.")}`}
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              {
                key: "address",
                label: phrase("Address"),
                render: (r) => (
                  <span className="flex items-center gap-2">
                    <code>{s(r.address)}</code> <CopyButton text={s(r.address)} label="address" />
                  </span>
                ),
              },
              {
                key: "messages",
                label: phrase("Mail"),
                render: (r) => (
                  <Link className="text-primary" to={to(`inbox?mailbox=${s(r.id)}`)}>
                    {s(r.messages)} {Number(r.unread) ? `(${s(r.unread)} ${phrase("unread")})` : ""}
                  </Link>
                ),
              },
              {
                key: "expires_at",
                label: phrase("Expires"),
                render: (r) => <Time value={r.expires_at} />,
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) => (
                  <ConfirmButton
                    title={phrase("Delete this temporary inbox?")}
                    description={`${s(r.address)} ${phrase("and all mail it received are deleted now.")}`}
                    confirm="Delete"
                    onConfirm={async () => {
                      await run(
                        () => api.send(`/temp-inboxes/${s(r.id)}`, "DELETE"),
                        phrase("Deleted."),
                      );
                      client.invalidateQueries({ queryKey: key("temp-inboxes") });
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
