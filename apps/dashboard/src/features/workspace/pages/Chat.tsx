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
  require_identity: boolean;
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
  require_identity: false,
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
  const [secret, setSecret] = useState<{ id: string; name: string; secret: string } | null>(null);
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
                    <ConfirmButton
                      title={phrase("Make a new identity secret?")}
                      description={phrase(
                        "The new secret is shown once. Signatures made with the old one stop working for new conversations at once, so update your website first or right after.",
                      )}
                      confirm="New secret"
                      destructive={false}
                      onConfirm={async () => {
                        const x = await run(
                          () =>
                            api.send<{ identity_secret: string }>(
                              `/chat/widgets/${s(r.id)}/identity-secret`,
                              "POST",
                            ),
                          "",
                        );
                        if (x)
                          setSecret({ id: s(r.id), name: s(r.name), secret: x.identity_secret });
                      }}
                    >
                      {phrase("Identity")}
                    </ConfirmButton>
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
                            require_identity: !!r.require_identity,
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
        onSaved={(created) => {
          setEditing(null);
          if (created) setSecret(created);
          refresh();
        }}
        api={api}
      />
      <Dialog open={!!secret} onOpenChange={(v) => !v && setSecret(null)}>
        <DialogContent className="max-w-2xl">
          <DialogTitle>{phrase("Identity secret")}</DialogTitle>
          <DialogDescription>
            {phrase(
              "Shown only now. Keep it on your server, never in a web page. With it your server can vouch for who a visitor is.",
            )}
          </DialogDescription>
          {secret && (
            <div className="grid gap-3">
              <pre className="overflow-auto rounded-lg border bg-muted p-3 text-xs">
                {secret.secret}
              </pre>
              <div className="flex justify-end">
                <CopyButton text={secret.secret} />
              </div>
              <p className="text-sm">
                {phrase(
                  "For a signed-in person, compute HMAC-SHA256 of their lower-case email with this secret (hex) and put it in the page before the script tag:",
                )}
              </p>
              <pre className="overflow-auto rounded-lg border bg-muted p-3 text-xs">{`<script>
  window.njiwaChat = { email: "neema@example.com", name: "Neema", identity: "<hex from your server>" };
</script>`}</pre>
            </div>
          )}
        </DialogContent>
      </Dialog>
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
  onSaved: (created?: { id: string; name: string; secret: string }) => void;
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
      require_identity: f.require_identity,
    };
    try {
      if (state?.id) {
        await api.send(`/chat/widgets/${state.id}`, "PUT", body);
        onSaved();
      } else {
        // The identity secret of a new chat is shown once, right after it is created.
        const r = await api.send<{ id: string; identity_secret: string }>(
          "/chat/widgets",
          "POST",
          body,
        );
        onSaved({ id: r.id, name: f.name.trim(), secret: r.identity_secret });
      }
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
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-1"
              checked={f.require_identity}
              onChange={(e) => setF({ ...f, require_identity: e.target.checked })}
            />
            <span>
              {phrase("Only identified visitors")}
              <span className="block text-xs text-muted-foreground">
                {phrase(
                  "Refuse visitors your website has not vouched for with a signed identity. See Identity on the chat.",
                )}
              </span>
            </span>
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
                        {r.identity_verified ? (
                          <span className="text-xs font-normal text-muted-foreground">✓</span>
                        ) : null}
                        {r.banned ? (
                          <span className="text-xs font-normal text-destructive">
                            {phrase("banned")}
                          </span>
                        ) : null}
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
  const snippets = useQuery({
    queryKey: key("chat-snippets"),
    queryFn: () => api.get<{ items: Row[] }>("/chat/snippets"),
  });
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
            {d.banned ? (
              <Button
                size="sm"
                variant="outline"
                onClick={async () => {
                  await run(
                    () => api.send(`/chat/conversations/${id}/ban`, "DELETE"),
                    phrase("Ban lifted"),
                  );
                  refresh();
                }}
              >
                {phrase("Lift ban")}
              </Button>
            ) : (
              <ConfirmButton
                title={phrase("Ban this visitor?")}
                description={phrase(
                  "The conversation is closed. This address and this email can no longer write or start a chat. You can lift the ban later.",
                )}
                confirm="Ban"
                onConfirm={async () => {
                  await run(
                    () => api.send(`/chat/conversations/${id}/ban`, "POST", {}),
                    phrase("Visitor banned"),
                  );
                  refresh();
                }}
              >
                {phrase("Ban")}
              </ConfirmButton>
            )}
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
              {d?.identity_verified ? ` · ${phrase("identified by the website")}` : ""}
              {d?.banned ? ` · ${phrase("banned")}` : ""}
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
            {(snippets.data?.items.length ?? 0) > 0 && (
              <select
                className={selectClass}
                aria-label={phrase("Insert a canned reply")}
                value=""
                onChange={(e) => {
                  const x = snippets.data?.items.find((i) => s(i.id) === e.target.value);
                  if (x) setText((t) => (t ? t + "\n" : "") + s(x.body));
                }}
              >
                <option value="">{phrase("Insert a canned reply…")}</option>
                {snippets.data?.items.map((i) => (
                  <option key={s(i.id)} value={s(i.id)}>
                    {s(i.title)}
                  </option>
                ))}
              </select>
            )}
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

// ---------------------------------------------------------------- canned replies

export function ChatSnippets() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useChatEnv();
  const client = useQueryClient();
  const [f, setF] = useState({ title: "", body: "" });
  const [editing, setEditing] = useState<string>("");
  const [error, setError] = useState("");
  const list = useQuery({
    queryKey: key("chat-snippets"),
    queryFn: () => api.get<{ items: Row[] }>("/chat/snippets"),
  });
  const refresh = () => client.invalidateQueries({ queryKey: key("chat-snippets") });
  const save = async () => {
    setError("");
    try {
      if (editing) await api.send(`/chat/snippets/${editing}`, "PUT", f);
      else await api.send("/chat/snippets", "POST", f);
      setF({ title: "", body: "" });
      setEditing("");
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <>
      <PageHeader
        title={phrase("Canned replies")}
        description={phrase(
          "Texts you use often. In a conversation, pick one to put it in your answer, then change it if you like.",
        )}
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} />
      </div>
      <Section title={phrase(editing ? "Edit canned reply" : "New canned reply")} className="mb-4">
        <div className="grid max-w-2xl gap-3">
          <Field label={phrase("Title")}>
            <input
              className={inputClass}
              value={f.title}
              onChange={(e) => setF({ ...f, title: e.target.value })}
            />
          </Field>
          <Field label={phrase("Text")}>
            <textarea
              className={textareaClass}
              rows={4}
              value={f.body}
              onChange={(e) => setF({ ...f, body: e.target.value })}
            />
          </Field>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button onClick={save} disabled={!f.title.trim() || !f.body.trim()}>
              {phrase("Save")}
            </Button>
            {editing && (
              <Button
                variant="outline"
                onClick={() => {
                  setEditing("");
                  setF({ title: "", body: "" });
                }}
              >
                {phrase("Cancel")}
              </Button>
            )}
          </div>
        </div>
      </Section>
      <DataState
        q={list}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No canned replies")}
              description={phrase("Add the answers you give again and again.")}
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              { key: "title", label: phrase("Title"), render: (r) => <b>{s(r.title)}</b> },
              {
                key: "body",
                label: phrase("Text"),
                render: (r) => <span className="line-clamp-2">{s(r.body)}</span>,
              },
              {
                key: "id",
                label: "",
                render: (r) => (
                  <span className="flex justify-end gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setEditing(s(r.id));
                        setF({ title: s(r.title), body: s(r.body) });
                      }}
                    >
                      {phrase("Edit")}
                    </Button>
                    <ConfirmButton
                      title={phrase("Delete this canned reply?")}
                      description={phrase("Answers already sent are not changed.")}
                      confirm="Delete"
                      onConfirm={async () => {
                        await run(() => api.send(`/chat/snippets/${s(r.id)}`, "DELETE"), "");
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
    </>
  );
}

// ---------------------------------------------------------------- analytics

const fmtSeconds = (v: unknown) => {
  if (v === null || v === undefined) return "—";
  const x = Math.round(Number(v));
  if (x < 90) return `${x} s`;
  if (x < 5400) return `${Math.round(x / 60)} min`;
  return `${(x / 3600).toFixed(1)} h`;
};

export function ChatAnalytics() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useChatEnv();
  const [days, setDays] = useParam("days", "30");
  const q = useQuery({
    queryKey: key("chat-analytics", days),
    queryFn: () => api.get<Row>(`/chat/analytics?days=${days || "30"}`),
  });
  return (
    <>
      <PageHeader
        title={phrase("Chat analytics")}
        description={phrase(
          "How many people wrote, how many you answered, and how long the first answer took.",
        )}
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <SendingMode value={env} onChange={setEnv} />
        <Field label={phrase("Period")}>
          <select
            className={selectClass}
            value={days || "30"}
            onChange={(e) => setDays(e.target.value)}
          >
            {["7", "30", "90"].map((x) => (
              <option key={x} value={x}>
                {`${x} ${phrase("days")}`}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <DataState q={q}>
        {(d) => {
          const t = (d.totals ?? {}) as Row;
          const m = (d.messages ?? {}) as Row;
          const cards: [string, string][] = [
            [phrase("Conversations"), s(t.conversations)],
            [phrase("Answered"), s(t.answered)],
            [phrase("Not answered"), s(t.unanswered)],
            [phrase("First answer, median"), fmtSeconds(t.first_answer_p50_seconds)],
            [phrase("First answer, slowest 10%"), fmtSeconds(t.first_answer_p90_seconds)],
            [phrase("Identified by the website"), s(t.identified)],
            [phrase("Messages from visitors"), s(m.from_visitors)],
            [phrase("Answers sent"), s(m.from_agents)],
            [phrase("Answers sent by email"), s(m.answers_emailed)],
          ];
          return (
            <>
              <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
                {cards.map(([label, value]) => (
                  <div key={label} className="rounded-xl border bg-card p-4">
                    <p className="text-xs text-muted-foreground">{label}</p>
                    <p className="mt-1 text-2xl font-semibold">{value}</p>
                  </div>
                ))}
              </div>
              <Section title={phrase("Conversations per day")}>
                {((d.daily as Row[]) ?? []).length === 0 ? (
                  <Empty title={phrase("No conversations in this period")} />
                ) : (
                  <Table
                    rows={d.daily as Row[]}
                    rowKey={(r) => s(r.day)}
                    columns={[
                      { key: "day", label: phrase("Day") },
                      { key: "conversations", label: phrase("Conversations") },
                    ]}
                  />
                )}
              </Section>
              <p className="mt-3 text-xs text-muted-foreground">
                {phrase(
                  "The first answer is timed from the visitor's first message to the first answer from an agent. A conversation nobody answered is counted, not timed.",
                )}
              </p>
            </>
          );
        }}
      </DataState>
    </>
  );
}
