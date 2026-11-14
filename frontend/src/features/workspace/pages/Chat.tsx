import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { cn } from "@/lib/utils";
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
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  Table,
  textareaClass,
  Time,
  useParam,
  type Page,
  type Row,
} from "../ui";

function useChatEnv() {
  const [env, setEnv] = useSendingModeParam();
  const ctx = useSendingEnvironment(env);
  return { env, setEnv, ...ctx };
}

const lines = (v: string) =>
  v
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean);

// ---------------------------------------------------------------- widgets

type Draft = {
  name: string;
  origins: string;
  title: string;
  welcome: string;
  offline: string;
  announcement: string;
  links: string; // "Label | https://…" per line
  reply_from: string;
  enabled: boolean;
};

const blank: Draft = {
  name: "",
  origins: "",
  title: "",
  welcome: "",
  offline: "",
  announcement: "",
  links: "",
  reply_from: "",
  enabled: true,
};

const parseLinks = (v: string) =>
  v
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const i = l.lastIndexOf("|");
      return i < 0
        ? { label: "", url: l }
        : { label: l.slice(0, i).trim(), url: l.slice(i + 1).trim() };
    });

const scriptTag = (id: string) =>
  `<script src="${window.location.origin}/v1/chat/${id}/widget.js" async></script>`;

export function ChatWidgets() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useChatEnv();
  const client = useQueryClient();
  const [editing, setEditing] = useState<{ id?: string; draft: Draft } | null>(null);
  const [embed, setEmbed] = useState<Row | null>(null);
  const list = useQuery({
    queryKey: key("chat-widgets"),
    queryFn: () => api.get<{ items: Row[] }>("/chat/widgets"),
  });
  const refresh = () => client.invalidateQueries({ queryKey: key("chat-widgets") });
  return (
    <>
      <PageHeader
        title={phrase("Live chat")}
        description={phrase(
          "Add a chat to your website with one script tag. Visitors write to you, you answer from the dashboard, and an answer reaches them by email if they have left.",
        )}
        action={
          <Button onClick={() => setEditing({ draft: blank })}>
            <Plus size={16} /> {phrase("New chat")}
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
              title={phrase("No chats")}
              description={phrase(
                "Create a chat, list the websites allowed to show it, then paste the script tag into your pages.",
              )}
              action={
                <Button onClick={() => setEditing({ draft: blank })}>{phrase("New chat")}</Button>
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
              { key: "conversations", label: phrase("Conversations") },
              { key: "unread", label: phrase("Unread") },
              {
                key: "id",
                label: "",
                render: (r) => (
                  <span className="flex flex-wrap justify-end gap-2">
                    <Button size="sm" variant="outline" onClick={() => setEmbed(r)}>
                      {phrase("Embed")}
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
                            title: s(r.title),
                            welcome: s(r.welcome),
                            offline: s(r.offline_message),
                            announcement: s(r.announcement),
                            links: ((r.links as { label: string; url: string }[]) ?? [])
                              .map((l) => `${l.label} | ${l.url}`)
                              .join("\n"),
                            reply_from: s(r.reply_from),
                            enabled: !!r.enabled,
                          },
                        })
                      }
                    >
                      {phrase("Edit")}
                    </Button>
                    <ConfirmButton
                      title={phrase("Delete this chat?")}
                      description={phrase(
                        "Its conversations are deleted permanently and the widget disappears from your pages.",
                      )}
                      confirm="Delete"
                      onConfirm={async () => {
                        await run(
                          () => api.send(`/chat/widgets/${s(r.id)}`, "DELETE"),
                          phrase("Chat deleted"),
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
      <WidgetDialog
        state={editing}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          refresh();
        }}
        api={api}
      />
      <Dialog open={!!embed} onOpenChange={(v) => !v && setEmbed(null)}>
        <DialogContent className="max-w-2xl">
          <DialogTitle>{phrase("Add the chat to your website")}</DialogTitle>
          <DialogDescription>
            {phrase(
              "Paste this one line before the closing body tag of every page that should show the chat. It works only on the websites you allowed.",
            )}
          </DialogDescription>
          {embed && (
            <>
              <pre className="overflow-auto rounded-lg border bg-muted p-3 text-xs">
                {scriptTag(s(embed.id))}
              </pre>
              <div className="flex justify-end">
                <CopyButton text={scriptTag(s(embed.id))} />
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function WidgetDialog({
  state,
  onClose,
  onSaved,
  api,
}: {
  state: { id?: string; draft: Draft } | null;
  onClose: () => void;
  onSaved: () => void;
  api: ReturnType<typeof useSendingEnvironment>["api"];
}) {
  const phrase = usePhrase();
  const [f, setF] = useState<Draft>(blank);
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
      title: f.title.trim(),
      welcome: f.welcome.trim(),
      offline_message: f.offline.trim(),
      announcement: f.announcement.trim(),
      links: parseLinks(f.links),
      reply_from: f.reply_from.trim(),
      enabled: f.enabled,
    };
    try {
      if (state?.id) await api.send(`/chat/widgets/${state.id}`, "PUT", body);
      else await api.send("/chat/widgets", "POST", body);
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Dialog open={!!state} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogTitle>{phrase(state?.id ? "Edit chat" : "New chat")}</DialogTitle>
        <DialogDescription>
          {phrase("Only the websites you list can show this chat.")}
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
          <Field label={phrase("Title")} hint={phrase("Shown at the top of the chat window.")}>
            <input
              className={inputClass}
              value={f.title}
              onChange={(e) => setF({ ...f, title: e.target.value })}
            />
          </Field>
          <Field label={phrase("Welcome text")}>
            <textarea
              className={textareaClass}
              rows={2}
              value={f.welcome}
              onChange={(e) => setF({ ...f, welcome: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Announcement")}
            hint={phrase("Optional notice on the home screen, for example opening hours.")}
          >
            <textarea
              className={textareaClass}
              rows={2}
              value={f.announcement}
              onChange={(e) => setF({ ...f, announcement: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Home screen links")}
            hint={phrase("One per line as: Label | https://address. Up to 8.")}
          >
            <textarea
              className={textareaClass}
              rows={3}
              value={f.links}
              onChange={(e) => setF({ ...f, links: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("When nobody is around")}
            hint={phrase("Optional text telling visitors when to expect an answer.")}
          >
            <input
              className={inputClass}
              value={f.offline}
              onChange={(e) => setF({ ...f, offline: e.target.value })}
            />
          </Field>
          <Field
            label={phrase("Send email answers from")}
            hint={phrase(
              "A verified sender address on your domain. Used when a visitor has left before the answer.",
            )}
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
              checked={f.enabled}
              onChange={(e) => setF({ ...f, enabled: e.target.checked })}
            />
            {phrase("Show the chat on your websites")}
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

// ---------------------------------------------------------------- conversations

export function ChatConversations() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useChatEnv();
  const { project } = useWorkspace();
  const client = useQueryClient();
  const [state, setState] = useParam("state");
  const [open, setOpen] = useParam("c");
  const params = new URLSearchParams({ limit: "50" });
  if (state) params.set("state", state);
  const page = useQuery({
    queryKey: key("chat-conversations", params.toString()),
    queryFn: () => api.get<Page>(`/chat/conversations?${params}`),
    refetchInterval: 30_000, // "online" is computed on the server; refresh it now and then
  });
  // A visitor's message arrives live (server-sent events); the browser resumes by itself.
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const es = new EventSource(
      `/admin/v1/w/${encodeURIComponent(project)}/${env}/chat/conversations/stream`,
      { withCredentials: true },
    );
    es.addEventListener("message", (ev) => {
      client.invalidateQueries({ queryKey: key("chat-conversations") });
      client.invalidateQueries({ queryKey: key("chat-widgets") });
      try {
        const d = JSON.parse((ev as MessageEvent).data) as { conversation: string };
        client.invalidateQueries({ queryKey: key("chat-conversation", d.conversation) });
      } catch {
        /* the list refresh above is enough */
      }
    });
    return () => es.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, env]);
  return (
    <>
      <PageHeader
        title={phrase("Conversations")}
        description={phrase(
          "Visitors who wrote to you through the chat. New messages appear as they arrive.",
        )}
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <SendingMode value={env} onChange={setEnv} />
        <Field label={phrase("State")}>
          <select className={selectClass} value={state} onChange={(e) => setState(e.target.value)}>
            <option value="">{phrase("All")}</option>
            {["open", "closed"].map((x) => (
              <option key={x} value={x}>
                {phrase(x)}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <div className="grid gap-4 lg:grid-cols-[22rem_1fr]">
        <DataState
          q={page}
          isEmpty={(d) => d.items.length === 0}
          empty={
            <div className="rounded-xl border bg-card">
              <Empty
                title={phrase("No conversations")}
                description={phrase("When a visitor writes through the chat, it appears here.")}
              />
            </div>
          }
        >
          {(d) => (
            <ul className="divide-y rounded-xl border bg-card">
              {d.items.map((r) => (
                <li key={s(r.id)}>
                  <button
                    className={cn(
                      "block w-full px-4 py-3 text-left hover:bg-muted",
                      open === s(r.id) && "bg-muted",
                    )}
                    onClick={() => setOpen(s(r.id))}
                  >
                    <span className="flex items-center justify-between gap-2">
                      <span className="flex items-center gap-2 font-medium">
                        <span
                          aria-label={phrase(r.online ? "online" : "offline")}
                          className={cn(
                            "inline-block h-2 w-2 rounded-full",
                            r.online ? "bg-green-500" : "bg-muted-foreground/40",
                          )}
                        />
                        {s(r.visitor) || s(r.email_masked) || phrase("Visitor")}
                      </span>
                      {n(r.unread) > 0 && (
                        <span className="rounded-full bg-primary px-1.5 text-xs text-primary-foreground">
                          {n(r.unread)}
                        </span>
                      )}
                    </span>
                    <span className="line-clamp-1 text-sm text-muted-foreground">
                      {s(r.preview)}
                    </span>
                    <span className="flex justify-between text-xs text-muted-foreground">
                      <span>{s(r.widget)}</span>
                      <Time value={r.last_message_at} relative />
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </DataState>
        {open ? (
          <Thread id={open} onGone={() => setOpen("")} />
        ) : (
          <div className="hidden rounded-xl border bg-card lg:block">
            <Empty
              title={phrase("Choose a conversation")}
              description={phrase("Open one on the left to read it and answer.")}
            />
          </div>
        )}
      </div>
    </>
  );
}

function Thread({ id, onGone }: { id: string; onGone: () => void }) {
  const phrase = usePhrase();
  const { api, key } = useChatEnv();
  const client = useQueryClient();
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const end = useRef<HTMLDivElement>(null);
  const q = useQuery({
    queryKey: key("chat-conversation", id),
    queryFn: () => api.get<Row>(`/chat/conversations/${id}`),
    refetchInterval: 15_000,
  });
  useEffect(() => {
    setText("");
    setError("");
  }, [id]);
  const msgs = (q.data?.messages as Row[]) ?? [];
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [msgs.length]);
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("chat-conversation", id) });
    client.invalidateQueries({ queryKey: key("chat-conversations") });
    client.invalidateQueries({ queryKey: key("chat-widgets") });
  };
  const send = async () => {
    setError("");
    try {
      await api.send(`/chat/conversations/${id}/messages`, "POST", { body: text });
      setText("");
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const setState = async (state: string) => {
    await run(() => api.send(`/chat/conversations/${id}`, "PATCH", { state }), phrase("Updated"));
    refresh();
  };
  const d = q.data;
  return (
    <Section
      title={d ? s(d.visitor) || s(d.email_masked) || phrase("Visitor") : phrase("Loading")}
      action={
        d && (
          <span className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setState(d.state === "open" ? "closed" : "open")}
            >
              {phrase(d.state === "open" ? "Close" : "Reopen")}
            </Button>
            <ConfirmButton
              title={phrase("Delete this conversation?")}
              description={phrase("The messages are deleted permanently.")}
              confirm="Delete"
              onConfirm={async () => {
                await run(
                  () => api.send(`/chat/conversations/${id}`, "DELETE"),
                  phrase("Conversation deleted"),
                );
                refresh();
                onGone();
              }}
            >
              <Trash2 size={14} />
            </ConfirmButton>
          </span>
        )
      }
    >
      <DataState q={q}>
        {() => (
          <div className="grid gap-3">
            <p className="text-xs text-muted-foreground">
              {s(d?.widget)} · {d?.online ? phrase("online now") : phrase("not on the page")}
              {s(d?.visitor_email) && ` · ${s(d?.visitor_email)}`}
              {!s(d?.visitor_email) &&
                ` · ${phrase("no email address: an answer cannot reach them after they leave")}`}
            </p>
            <div className="grid max-h-[28rem] gap-2 overflow-y-auto rounded-lg border bg-muted/30 p-3">
              {msgs.map((m) => {
                const mine = s(m.sender) === "agent";
                return (
                  <div key={s(m.id)} className={cn("flex flex-col", mine && "items-end")}>
                    <div
                      className={cn(
                        "max-w-[85%] whitespace-pre-wrap break-words rounded-xl px-3 py-2 text-sm",
                        mine ? "bg-primary text-primary-foreground" : "bg-card border",
                      )}
                    >
                      {s(m.body)}
                    </div>
                    <span className="mt-0.5 text-xs text-muted-foreground">
                      <Time value={m.at} relative />
                      {mine && (
                        <>
                          {" · "}
                          {m.emailed_at
                            ? phrase("sent by email")
                            : m.delivered_at
                              ? phrase("seen on the page")
                              : phrase("waiting for the visitor")}
                        </>
                      )}
                    </span>
                  </div>
                );
              })}
              <div ref={end} />
            </div>
            <textarea
              className={textareaClass}
              rows={3}
              value={text}
              placeholder={phrase("Write an answer")}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && text.trim()) send();
              }}
            />
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <div className="flex justify-end">
              <Button onClick={send} disabled={!text.trim()}>
                {phrase("Send answer")}
              </Button>
            </div>
          </div>
        )}
      </DataState>
    </Section>
  );
}
