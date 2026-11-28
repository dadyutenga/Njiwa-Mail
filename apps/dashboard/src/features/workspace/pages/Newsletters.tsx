import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useSendingEnvironment, useWorkspace } from "../context";
import { SendingMode, useSendingModeParam, withSendingMode } from "../SendingMode";
import {
  ConfirmButton,
  DataState,
  Empty,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  Table,
  textareaClass,
  Time,
  type Page,
  type Row,
} from "../ui";

export type Block = { type: string; text?: string; url?: string; alt?: string };

type Issue = {
  name: string;
  subject: string;
  preview_text: string;
  sender_email: string;
  list_id: string;
  segment_id: string;
  blocks: Block[];
  track: boolean;
};

const blockNames: Record<string, string> = {
  heading: "Heading",
  text: "Text",
  image: "Image",
  button: "Button",
  divider: "Divider",
  footer: "Footer",
};

const blank: Issue = {
  name: "",
  subject: "",
  preview_text: "",
  sender_email: "",
  list_id: "",
  segment_id: "",
  blocks: [
    { type: "heading", text: "" },
    { type: "text", text: "" },
  ],
  track: false,
};

function usePage() {
  const { to: workspaceTo } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const sending = useSendingEnvironment(env);
  const to = (path: string) =>
    withSendingMode(
      workspaceTo(path),
      /^(newsletters|bulk|messages)(\/|\?|$)/.test(path) ? env : "live",
    );
  return { env, setEnv, to, ...sending };
}

export function Newsletters() {
  const phrase = usePhrase();
  const { env, setEnv, to, api, key, info } = usePage();
  const q = useQuery({
    queryKey: key("newsletters"),
    queryFn: () => api.get<Page>("/newsletters?limit=100"),
    refetchInterval: 20_000,
  });
  return (
    <>
      <PageHeader
        title={phrase("Newsletters")}
        description={phrase(
          "Write an issue, send yourself a test, then send or schedule it to a list. Every copy has an unsubscribe link.",
        )}
        action={
          <Button asChild>
            <Link to={to("newsletters/new")}>
              <Plus size={16} /> {phrase("New newsletter")}
            </Link>
          </Button>
        }
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} disabled={!info} />
      </div>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <Empty title={phrase("No newsletters")} description={phrase("Create the first issue.")} />
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              {
                key: "name",
                label: phrase("Name"),
                render: (r) => (
                  <Link className="font-medium text-primary" to={to(`newsletters/${s(r.id)}`)}>
                    {s(r.name)}
                  </Link>
                ),
              },
              {
                key: "state",
                label: phrase("State"),
                render: (r) => <StateBadge state={s(r.state)} />,
              },
              {
                key: "send_at",
                label: phrase("Sends at"),
                render: (r) => <Time value={r.send_at} />,
              },
              { key: "results", label: phrase("Results"), render: (r) => <Results r={r} /> },
            ]}
          />
        )}
      </DataState>
    </>
  );
}

/** Sent, bounced, unsubscribed, and opens and clicks when tracking is on. */
export function Results({ r }: { r: Row }) {
  const phrase = usePhrase();
  if (!r.campaign_id) return <span className="text-muted-foreground">—</span>;
  const parts: [string, unknown][] = [
    ["Recipients", r.recipients],
    ["Sent", r.sent],
    ["Opened", r.opened],
    ["Clicked", r.clicked],
    ["Unsubscribed", r.unsubscribed],
    ["Bounced", r.bounced],
  ];
  return (
    <span className="text-xs">
      {parts
        .filter(([, v]) => v !== null && v !== undefined)
        .map(([k, v]) => `${phrase(k)}: ${s(v)}`)
        .join(" · ")}
    </span>
  );
}

export function NewsletterEditor() {
  const { id = "new" } = useParams();
  const page = usePage();
  const q = useQuery({
    queryKey: page.key("newsletter", id),
    queryFn: () => page.api.get<Row>(`/newsletters/${id}`),
    enabled: id !== "new",
  });
  if (id === "new") return <Editor key="new" id="" saved={blank} />;
  return (
    <DataState q={q}>
      {(r) => (
        <Editor
          key={id}
          id={id}
          row={r}
          saved={{
            name: s(r.name),
            subject: s(r.subject),
            preview_text: s(r.preview_text),
            sender_email: s(r.sender_email),
            list_id: s(r.list_id),
            segment_id: s(r.segment_id),
            blocks: (r.blocks as Block[]) ?? [],
            track: r.track === true,
          }}
        />
      )}
    </DataState>
  );
}

function Editor({ id, saved, row }: { id: string; saved: Issue; row?: Row }) {
  const phrase = usePhrase();
  const { env, setEnv, to, api, key, info } = usePage();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [f, setF] = useState<Issue>(saved);
  const [dirty, setDirty] = useState(!id);
  const [error, setError] = useState("");
  const [when, setWhen] = useState("");
  const [phone, setPhone] = useState(false);
  const [preview, setPreview] = useState({ html: "", text: "" });
  const sent = !!row?.campaign_id;
  const set = (patch: Partial<Issue>) => {
    setF((x) => ({ ...x, ...patch }));
    setDirty(true);
  };
  const setBlock = (i: number, patch: Partial<Block>) =>
    set({ blocks: f.blocks.map((b, j) => (j === i ? { ...b, ...patch } : b)) });
  const move = (i: number, by: number) => {
    const blocks = [...f.blocks];
    [blocks[i], blocks[i + by]] = [blocks[i + by], blocks[i]];
    set({ blocks });
  };
  const senders = useQuery({
    queryKey: key("bulk-senders"),
    queryFn: () => api.get<{ items: Row[] }>("/bulk-senders"),
  });
  const lists = useQuery({
    queryKey: key("lists"),
    queryFn: () => api.get<{ items: Row[] }>("/lists"),
  });
  const segments = useQuery({
    queryKey: key("segments"),
    queryFn: () => api.get<{ items: Row[] }>("/segments"),
  });
  const content = JSON.stringify([f.preview_text, f.blocks]);
  useEffect(() => {
    let live = true;
    const t = setTimeout(async () => {
      try {
        const r = await api.send<{ html: string; text: string }>("/newsletters/preview", "POST", f);
        if (live && r) setPreview(r);
      } catch {
        /* an unfinished block: keep the last preview */
      }
    }, 400);
    return () => {
      live = false;
      clearTimeout(t);
    };
    // Only what the preview shows triggers a new one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [content]);

  const save = async () => {
    setError("");
    try {
      const r = await api.send<{ id: string }>(
        id ? `/newsletters/${id}` : "/newsletters",
        id ? "PUT" : "POST",
        f,
      );
      setDirty(false);
      client.invalidateQueries({ queryKey: key("newsletters") });
      if (!id) navigate(to(`newsletters/${r.id}`), { replace: true });
      return true;
    } catch (e) {
      setError(errorMessage(e));
      return false;
    }
  };
  const send = async () => {
    setError("");
    try {
      await api.send(`/newsletters/${id}/send`, "POST", {
        send_at: when ? new Date(when).toISOString() : "",
      });
      client.invalidateQueries({ queryKey: key("newsletter", id) });
      client.invalidateQueries({ queryKey: key("newsletters") });
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const audience = f.list_id ? `list:${f.list_id}` : f.segment_id ? `segment:${f.segment_id}` : "";
  return (
    <>
      <PageHeader
        back={{ to: to("newsletters"), label: phrase("Newsletters") }}
        title={f.name || phrase("New newsletter")}
        description={
          row ? (
            <>
              <StateBadge state={s(row.state)} /> <Results r={row} />{" "}
              {sent && (
                <Link className="text-primary underline" to={to(`bulk/${s(row.campaign_id)}`)}>
                  {phrase("View delivery")}
                </Link>
              )}
            </>
          ) : undefined
        }
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} disabled={!info || !!id} />
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <fieldset disabled={sent} className="space-y-4">
          <Section title={phrase("Details")}>
            <div className="space-y-3">
              <Field label={phrase("Name")}>
                <input
                  className={inputClass}
                  value={f.name}
                  onChange={(e) => set({ name: e.target.value })}
                />
              </Field>
              <Field label={phrase("Subject")}>
                <input
                  className={inputClass}
                  value={f.subject}
                  onChange={(e) => set({ subject: e.target.value })}
                />
              </Field>
              <Field
                label={phrase("Preview text")}
                hint={phrase("Shown next to the subject in many inboxes.")}
              >
                <input
                  className={inputClass}
                  value={f.preview_text}
                  onChange={(e) => set({ preview_text: e.target.value })}
                />
              </Field>
              <Field
                label={phrase("Sender")}
                hint={phrase("Newsletters come from a sender on a verified bulk domain.")}
              >
                <select
                  className={selectClass}
                  value={f.sender_email}
                  onChange={(e) => set({ sender_email: e.target.value })}
                >
                  <option value="">{phrase("Choose a sender")}</option>
                  {(senders.data?.items ?? []).map((x) => (
                    <option key={s(x.id)} value={s(x.email)}>
                      {s(x.email)}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label={phrase("Audience")}>
                <select
                  className={selectClass}
                  value={audience}
                  onChange={(e) => {
                    const [kind, value] = e.target.value.split(":");
                    set({
                      list_id: kind === "list" ? value : "",
                      segment_id: kind === "segment" ? value : "",
                    });
                  }}
                >
                  <option value="">{phrase("Choose a list or segment")}</option>
                  {(lists.data?.items ?? []).map((x) => (
                    <option key={s(x.id)} value={`list:${s(x.id)}`}>
                      {phrase("List")}: {s(x.name)}
                    </option>
                  ))}
                  {(segments.data?.items ?? []).map((x) => (
                    <option key={s(x.id)} value={`segment:${s(x.id)}`}>
                      {phrase("Segment")}: {s(x.name)}
                    </option>
                  ))}
                </select>
              </Field>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={f.track}
                  onChange={(e) => set({ track: e.target.checked })}
                />
                {phrase("Track opens and clicks")}
              </label>
            </div>
          </Section>
          <Section title={phrase("Content")}>
            <ol className="space-y-3">
              {f.blocks.map((b, i) => (
                <li key={i} className="rounded-lg border p-3" data-block={b.type}>
                  <div className="mb-2 flex items-center gap-1">
                    <span className="mr-auto text-sm font-medium">
                      {phrase(blockNames[b.type] ?? b.type)}
                    </span>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={phrase("Move up")}
                      disabled={i === 0}
                      onClick={() => move(i, -1)}
                    >
                      <ArrowUp size={14} />
                    </Button>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={phrase("Move down")}
                      disabled={i === f.blocks.length - 1}
                      onClick={() => move(i, 1)}
                    >
                      <ArrowDown size={14} />
                    </Button>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={phrase("Remove block")}
                      onClick={() => set({ blocks: f.blocks.filter((_, j) => j !== i) })}
                    >
                      <Trash2 size={14} />
                    </Button>
                  </div>
                  {["heading", "text", "footer", "button"].includes(b.type) && (
                    <textarea
                      aria-label={`${phrase(blockNames[b.type])} ${i + 1}`}
                      className={textareaClass}
                      rows={b.type === "text" ? 4 : 1}
                      value={b.text ?? ""}
                      onChange={(e) => setBlock(i, { text: e.target.value })}
                    />
                  )}
                  {(b.type === "button" || b.type === "image") && (
                    <input
                      aria-label={`${phrase("Web address")} ${i + 1}`}
                      className={inputClass + " mt-2"}
                      placeholder="https://"
                      value={b.url ?? ""}
                      onChange={(e) => setBlock(i, { url: e.target.value })}
                    />
                  )}
                  {b.type === "image" && (
                    <input
                      aria-label={`${phrase("Image description")} ${i + 1}`}
                      className={inputClass + " mt-2"}
                      placeholder={phrase("Image description")}
                      value={b.alt ?? ""}
                      onChange={(e) => setBlock(i, { alt: e.target.value })}
                    />
                  )}
                </li>
              ))}
              <li className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
                {phrase(
                  "The unsubscribe link is added to every copy automatically and cannot be removed.",
                )}
              </li>
            </ol>
            <div className="mt-3 flex flex-wrap gap-2">
              {Object.entries(blockNames).map(([type, label]) => (
                <Button
                  key={type}
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => set({ blocks: [...f.blocks, { type }] })}
                >
                  <Plus size={14} /> {phrase(label)}
                </Button>
              ))}
            </div>
          </Section>
        </fieldset>
        <div className="space-y-4">
          <Section
            title={phrase("Preview")}
            action={
              <Button size="sm" variant="outline" onClick={() => setPhone(!phone)}>
                {phrase(phone ? "Desktop width" : "Phone width")}
              </Button>
            }
          >
            <iframe
              title={phrase("Preview")}
              sandbox=""
              className="mx-auto h-[30rem] w-full rounded border bg-white"
              style={phone ? { maxWidth: 375 } : undefined}
              srcDoc={
                preview.html +
                '<p style="font:12px Arial;color:#666;text-align:center">' +
                "Unsubscribe link (added when sent)</p>"
              }
            />
            <details className="mt-2 text-sm">
              <summary className="cursor-pointer">{phrase("Plain-text version")}</summary>
              <pre className="mt-2 whitespace-pre-wrap rounded bg-muted p-2 text-xs">
                {preview.text}
              </pre>
            </details>
          </Section>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          {!sent && (
            <Section title={phrase("Send")}>
              <div className="flex flex-wrap items-end gap-2">
                <Button onClick={() => void save()} disabled={!f.name.trim() || !dirty}>
                  {phrase("Save draft")}
                </Button>
                <Button
                  variant="outline"
                  disabled={!id || dirty}
                  onClick={() =>
                    run(
                      () => api.send(`/newsletters/${id}/test`, "POST"),
                      phrase("Test sent to your address."),
                    )
                  }
                >
                  {phrase("Send me a test")}
                </Button>
                <Field label={phrase("Send time (empty: now)")}>
                  <input
                    type="datetime-local"
                    className={inputClass}
                    value={when}
                    onChange={(e) => setWhen(e.target.value)}
                  />
                </Field>
                <ConfirmButton
                  size="default"
                  disabled={!id || dirty || !audience}
                  destructive={env === "live"}
                  title={phrase(when ? "Schedule this newsletter?" : "Send this newsletter now?")}
                  description={phrase(
                    env === "live"
                      ? "It goes to real recipients (Live). Suppressed and unsubscribed contacts are left out."
                      : "It is captured in the Sandbox. Suppressed and unsubscribed contacts are left out.",
                  )}
                  confirm={when ? "Schedule" : "Send now"}
                  onConfirm={send}
                >
                  {phrase(when ? "Schedule" : "Send now")}
                </ConfirmButton>
                {id && (
                  <ConfirmButton
                    size="default"
                    title={phrase("Delete this draft?")}
                    description={phrase("The draft is removed. Nothing was sent.")}
                    confirm="Delete"
                    onConfirm={async () => {
                      await run(() => api.send(`/newsletters/${id}`, "DELETE"), phrase("Deleted."));
                      navigate(to("newsletters"));
                    }}
                  >
                    {phrase("Delete")}
                  </ConfirmButton>
                )}
              </div>
              {dirty && id && (
                <p className="mt-2 text-xs text-muted-foreground">
                  {phrase("Save the draft before testing or sending.")}
                </p>
              )}
            </Section>
          )}
        </div>
      </div>
    </>
  );
}
