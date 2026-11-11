import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Bold, Italic, Link2, List, Paperclip, Trash2, X } from "lucide-react";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import {
  emailOk,
  errorMessage,
  Field,
  inputClass,
  selectClass,
  s,
  Time,
  type Page,
  type Row,
} from "../ui";

export type ComposePrefill = {
  to?: string;
  cc?: string;
  subject?: string;
  html?: string;
  text?: string;
  from?: string;
  inReplyTo?: string;
  draftId?: string;
  template?: string;
};

type Attachment = { filename: string; content_type: string; content: string; size: number };
const allowedTypes: Record<string, string> = {
  "application/pdf": ".pdf",
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "text/plain": ".txt",
};

const splitAddrs = (v: string) =>
  v
    .split(/[,;\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);

function escapeHtml(t: string) {
  return t.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function RichText({
  initial,
  onChange,
  label,
}: {
  initial: string;
  onChange: (html: string) => void;
  label: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.innerHTML = initial;
    // initial content only; later edits come from the user
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const cmd = (c: string, v?: string) => {
    ref.current?.focus();
    document.execCommand(c, false, v);
    onChange(ref.current?.innerHTML ?? "");
  };
  return (
    <div className="rounded-md border border-input focus-within:ring-2 focus-within:ring-ring">
      <div className="flex gap-1 border-b p-1" role="toolbar" aria-label="Formatting">
        <Button
          type="button"
          size="icon"
          variant="ghost"
          aria-label="Bold"
          onClick={() => cmd("bold")}
        >
          <Bold size={15} />
        </Button>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          aria-label="Italic"
          onClick={() => cmd("italic")}
        >
          <Italic size={15} />
        </Button>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          aria-label="Bulleted list"
          onClick={() => cmd("insertUnorderedList")}
        >
          <List size={15} />
        </Button>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          aria-label="Link"
          onClick={() => {
            const url = window.prompt("Link address (https://…)");
            if (url && /^https?:\/\//.test(url)) cmd("createLink", url);
          }}
        >
          <Link2 size={15} />
        </Button>
      </div>
      <div
        ref={ref}
        role="textbox"
        aria-multiline="true"
        aria-label={label}
        contentEditable
        suppressContentEditableWarning
        className="prose prose-sm min-h-48 max-w-none p-3 text-sm focus:outline-none dark:prose-invert"
        onInput={() => onChange(ref.current?.innerHTML ?? "")}
      />
    </div>
  );
}

type TemplateRow = {
  id: string;
  slug: string;
  published_version: number | null;
  archived: boolean;
};
type TemplateDetail = {
  versions: {
    version: number;
    published_at: string | null;
    variable_schema: { name: string; required?: boolean }[];
  }[];
};

export function ComposePanel({
  open,
  onOpenChange,
  prefill,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  prefill?: ComposePrefill;
}) {
  const phrase = usePhrase();
  const { api, env, to: path, info } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const navigate = useNavigate();
  // One idempotency key per panel opening: a double click cannot send twice.
  const [idemKey] = useState(() => "ui-" + crypto.randomUUID());
  const [tab, setTab] = useState<"write" | "drafts">("write");
  const [from, setFrom] = useState(prefill?.from ?? "");
  const [toV, setTo] = useState(prefill?.to ?? "");
  const [cc, setCc] = useState(prefill?.cc ?? "");
  const [bcc, setBcc] = useState("");
  const [showCc, setShowCc] = useState(!!prefill?.cc);
  const [subject, setSubject] = useState(prefill?.subject ?? "");
  const [html, setHtml] = useState(
    prefill?.html ?? (prefill?.text ? escapeHtml(prefill.text).replace(/\n/g, "<br>") : ""),
  );
  const [template, setTemplate] = useState(prefill?.template ?? "");
  const [vars, setVars] = useState<Record<string, string>>({});
  const [sendAt, setSendAt] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [draftId, setDraftId] = useState(prefill?.draftId ?? "");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [blocked, setBlocked] = useState<{ email: string; reason: string }[]>([]);
  const [busy, setBusy] = useState(false);
  const [editorKey, setEditorKey] = useState(0);

  const senders = useQuery({
    queryKey: key("senders"),
    queryFn: () => api.get<Page<Row>>("/senders?limit=100"),
    enabled: open,
  });
  const verified = (senders.data?.items ?? []).filter((r) => r.status === "verified");
  useEffect(() => {
    if (!from && verified.length) {
      const def = verified.find((r) => r.is_default) ?? verified[0];
      setFrom(s(def.email));
    }
  }, [verified, from]);

  const templates = useQuery({
    queryKey: key("templates", "compose"),
    queryFn: () => api.get<Page<TemplateRow>>("/templates?limit=100"),
    enabled: open,
  });
  const chosen = templates.data?.items.find((t) => t.slug === template);
  const tplDetail = useQuery({
    queryKey: key("template", chosen?.id),
    queryFn: () => api.get<TemplateDetail>(`/templates/${chosen?.id}`),
    enabled: !!chosen,
  });
  const schema = useMemo(
    () => tplDetail.data?.versions.find((v) => v.published_at)?.variable_schema ?? [],
    [tplDetail.data],
  );

  const drafts = useQuery({
    queryKey: key("drafts"),
    queryFn: () => api.get<Page<Row>>("/drafts?limit=50"),
    enabled: open && tab === "drafts",
  });

  // Load a draft passed in the prefill.
  useEffect(() => {
    if (!prefill?.draftId) return;
    api.get<Record<string, unknown>>(`/drafts/${prefill.draftId}`).then(
      (d) => applyDraft(d),
      () => toast.error(phrase("The draft could not be opened.")),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const applyDraft = (d: Record<string, unknown>) => {
    setDraftId(s(d.id));
    setFrom(s(d.from));
    setTo(s(d.to));
    setCc(s(d.cc));
    setBcc(s(d.bcc));
    setSubject(s(d.subject));
    setHtml(s(d.html));
    setTemplate(s(d.template));
    setEditorKey((k) => k + 1);
    setTab("write");
  };

  // Autosave a draft 3 seconds after the last change.
  const snapshot = JSON.stringify({
    from,
    to: toV,
    cc,
    bcc,
    subject,
    html,
    template,
    in_reply_to: prefill?.inReplyTo ?? "",
  });
  const saved = useRef(snapshot);
  const draftRef = useRef(draftId);
  draftRef.current = draftId;
  useEffect(() => {
    if (!open || snapshot === saved.current || (!toV && !subject && !html)) return;
    const t = setTimeout(async () => {
      try {
        const body = JSON.parse(snapshot);
        const r = draftRef.current
          ? await api.send<{ id: string }>(`/drafts/${draftRef.current}`, "PUT", body)
          : await api.send<{ id: string }>("/drafts", "POST", body);
        setDraftId(r.id);
        saved.current = snapshot;
        client.invalidateQueries({ queryKey: key("drafts") });
      } catch {
        /* autosave is best effort; sending still works */
      }
    }, 3000);
    return () => clearTimeout(t);
  }, [snapshot, open, api, client, key, toV, subject, html]);

  const validate = () => {
    const e: Record<string, string> = {};
    if (!from) e.from = phrase("Choose a sender.");
    const all = [toV, ...(showCc ? [cc, bcc] : [])];
    if (!emailOk(toV)) e.to = phrase("Enter one email address.");
    for (const v of splitAddrs(all.slice(1).join(",")))
      if (!emailOk(v)) e.cc = `${phrase("Not an email address:")} ${v}`;
    if (!template && !subject.trim()) e.subject = phrase("Enter a subject.");
    if (!template && !html.replace(/<[^>]*>/g, "").trim()) e.body = phrase("Write a message.");
    for (const v of schema)
      if (v.required && !vars[v.name]) e["var_" + v.name] = phrase("Required");
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const checkRecipients = async () => {
    const emails = [toV, ...splitAddrs(cc), ...splitAddrs(bcc)].filter(emailOk);
    if (!emails.length) return [];
    try {
      const r = await api.send<{ blocked: { email: string; reason: string }[] }>(
        "/recipients/check",
        "POST",
        { emails },
      );
      setBlocked(r.blocked);
      return r.blocked;
    } catch {
      return [];
    }
  };

  const addFiles = async (files: FileList | null) => {
    if (!files) return;
    const next = [...attachments];
    for (const f of Array.from(files)) {
      const type = f.type === "image/jpg" ? "image/jpeg" : f.type;
      if (!allowedTypes[type]) {
        toast.error(
          `${f.name}: ${phrase("only PDF, PNG, JPEG and plain text files can be attached.")}`,
        );
        continue;
      }
      if (next.reduce((a, x) => a + x.size, 0) + f.size > 5 * 1024 * 1024) {
        toast.error(phrase("Attachments may total at most 5 MB."));
        break;
      }
      const data = await new Promise<string>((resolve, reject) => {
        const rd = new FileReader();
        rd.onload = () => resolve(String(rd.result).split(",")[1] ?? "");
        rd.onerror = () => reject(rd.error);
        rd.readAsDataURL(f);
      });
      next.push({ filename: f.name, content_type: type, content: data, size: f.size });
    }
    setAttachments(next.slice(0, 5));
  };

  const send = async () => {
    if (!validate()) return;
    setBusy(true);
    try {
      const b = await checkRecipients();
      if (b.length) {
        setErrors((e) => ({ ...e, to: phrase("Remove the blocked addresses listed below.") }));
        return;
      }
      const body: Record<string, unknown> = {
        from,
        to: toV.trim(),
        cc: splitAddrs(cc),
        bcc: splitAddrs(bcc),
        attachments: attachments.map(({ filename, content_type, content }) => ({
          filename,
          content_type,
          content,
        })),
        in_reply_to: prefill?.inReplyTo ?? "",
        draft_id: draftId,
      };
      if (template) {
        body.template = template;
        body.variables = vars;
      } else {
        body.subject = subject.trim();
        body.html = html;
      }
      if (sendAt) body.send_at = new Date(sendAt).toISOString();
      const r = await api.sendWithKey<{ id: string; status: string }>("/compose", idemKey, body);
      toast.success(
        r.status === "scheduled"
          ? phrase("Scheduled.")
          : env === "sandbox"
            ? phrase("Sent to the sandbox. Nothing left the server.")
            : phrase("Queued for sending."),
        { action: { label: phrase("Open"), onClick: () => navigate(path(`messages/${r.id}`)) } },
      );
      client.invalidateQueries({ queryKey: key("messages") });
      client.invalidateQueries({ queryKey: key("drafts") });
      onOpenChange(false);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const noSender = senders.isSuccess && verified.length === 0;
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetTitle>{prefill?.inReplyTo ? phrase("Reply") : phrase("Compose")}</SheetTitle>
        <SheetDescription>
          {env === "sandbox"
            ? phrase("Sandbox: the message is captured for review and nothing leaves the server.")
            : phrase("Live: the message goes to real recipients.")}
        </SheetDescription>
        <div className="mt-3 flex gap-2 border-b" role="tablist">
          {(["write", "drafts"] as const).map((t) => (
            <button
              key={t}
              role="tab"
              aria-selected={tab === t}
              onClick={() => setTab(t)}
              className={`-mb-px border-b-2 px-3 py-2 text-sm ${tab === t ? "border-primary font-semibold" : "border-transparent text-muted-foreground"}`}
            >
              {t === "write" ? phrase("Write") : phrase("Drafts")}
            </button>
          ))}
        </div>
        {tab === "drafts" ? (
          <ul className="mt-4 space-y-2">
            {drafts.isPending && (
              <li className="text-sm text-muted-foreground">{phrase("Loading…")}</li>
            )}
            {drafts.data?.items.length === 0 && (
              <li className="text-sm text-muted-foreground">
                {phrase("No drafts. Drafts are saved automatically while you write.")}
              </li>
            )}
            {drafts.data?.items.map((d) => (
              <li key={s(d.id)} className="flex items-center gap-2 rounded-lg border p-3">
                <button
                  className="min-w-0 flex-1 text-left"
                  onClick={() =>
                    api
                      .get<Record<string, unknown>>(`/drafts/${s(d.id)}`)
                      .then(applyDraft, (e) => toast.error(errorMessage(e)))
                  }
                >
                  <span className="block truncate font-medium">
                    {s(d.subject) || phrase("(no subject)")}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    <Time value={d.updated_at} />
                  </span>
                </button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={phrase("Delete draft")}
                  onClick={async () => {
                    await api.send(`/drafts/${s(d.id)}`, "DELETE");
                    if (draftId === s(d.id)) setDraftId("");
                    client.invalidateQueries({ queryKey: key("drafts") });
                  }}
                >
                  <Trash2 size={15} />
                </Button>
              </li>
            ))}
          </ul>
        ) : noSender ? (
          <div className="mt-6 rounded-xl border p-5 text-sm">
            <p className="font-semibold">{phrase("No verified sender address yet")}</p>
            <p className="mt-1 text-muted-foreground">
              {phrase(
                "Mail can only be sent from an address on a verified domain of this project.",
              )}
            </p>
            <Link
              className="mt-3 inline-block font-medium text-primary"
              to={path("senders")}
              onClick={() => onOpenChange(false)}
            >
              {phrase("Go to Sender addresses →")}
            </Link>
          </div>
        ) : (
          <form
            className="mt-4 space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
          >
            <Field label={phrase("From")} error={errors.from}>
              <select
                className={selectClass}
                value={from}
                onChange={(e) => setFrom(e.target.value)}
              >
                {verified.map((r) => (
                  <option key={s(r.id)} value={s(r.email)}>
                    {r.display_name ? `${s(r.display_name)} <${s(r.email)}>` : s(r.email)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={phrase("To")} error={errors.to}>
              <div className="flex gap-2">
                <input
                  className={inputClass}
                  type="email"
                  value={toV}
                  onChange={(e) => {
                    setTo(e.target.value);
                    if (errors.to)
                      setErrors({ ...errors, to: emailOk(e.target.value) ? "" : errors.to });
                  }}
                  onBlur={() => {
                    if (toV && !emailOk(toV))
                      setErrors({ ...errors, to: phrase("Enter one email address.") });
                    else void checkRecipients();
                  }}
                />
                {!showCc && (
                  <Button type="button" variant="ghost" size="sm" onClick={() => setShowCc(true)}>
                    Cc/Bcc
                  </Button>
                )}
              </div>
            </Field>
            {showCc && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field
                  label="Cc"
                  error={errors.cc}
                  hint={phrase("Separate addresses with commas.")}
                >
                  <input
                    className={inputClass}
                    value={cc}
                    onChange={(e) => setCc(e.target.value)}
                    onBlur={() => void checkRecipients()}
                  />
                </Field>
                <Field label="Bcc">
                  <input
                    className={inputClass}
                    value={bcc}
                    onChange={(e) => setBcc(e.target.value)}
                    onBlur={() => void checkRecipients()}
                  />
                </Field>
              </div>
            )}
            {blocked.length > 0 && (
              <div
                role="alert"
                className="rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm"
              >
                <p className="font-medium">{phrase("These addresses will not receive mail:")}</p>
                <ul className="mt-1 list-disc pl-5">
                  {blocked.map((b) => (
                    <li key={b.email}>
                      {b.email} —{" "}
                      {b.reason === "suppressed" ? (
                        <Link className="text-primary underline" to={path("suppressions")}>
                          {phrase("suppressed")}
                        </Link>
                      ) : b.reason === "not_on_live_allowlist" ? (
                        phrase("not on this environment's approved recipient list")
                      ) : (
                        phrase(b.reason)
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            <Field
              label={phrase("Template (optional)")}
              hint={phrase("Use a published template instead of writing the message.")}
            >
              <select
                className={selectClass}
                value={template}
                onChange={(e) => setTemplate(e.target.value)}
              >
                <option value="">{phrase("No template — write the message")}</option>
                {templates.data?.items
                  .filter((t) => t.published_version && !t.archived)
                  .map((t) => (
                    <option key={t.id} value={t.slug}>
                      {t.slug}
                    </option>
                  ))}
              </select>
            </Field>
            {template ? (
              <div className="grid gap-3 sm:grid-cols-2">
                {schema.map((v) => (
                  <Field
                    key={v.name}
                    label={`${v.name}${v.required ? " *" : ""}`}
                    error={errors["var_" + v.name]}
                  >
                    <input
                      className={inputClass}
                      value={vars[v.name] ?? ""}
                      onChange={(e) => setVars({ ...vars, [v.name]: e.target.value })}
                    />
                  </Field>
                ))}
                {!schema.length && (
                  <p className="text-sm text-muted-foreground">
                    {phrase("This template has no variables.")}
                  </p>
                )}
              </div>
            ) : (
              <>
                <Field label={phrase("Subject")} error={errors.subject}>
                  <input
                    className={inputClass}
                    value={subject}
                    onChange={(e) => setSubject(e.target.value)}
                  />
                </Field>
                <div>
                  <span className="mb-1.5 block text-sm font-medium">{phrase("Message")}</span>
                  <RichText
                    key={editorKey}
                    initial={html}
                    onChange={setHtml}
                    label={phrase("Message")}
                  />
                  {errors.body && (
                    <p role="alert" className="mt-1 text-xs text-destructive">
                      {errors.body}
                    </p>
                  )}
                  <p className="mt-1 text-xs text-muted-foreground">
                    {phrase("A plain-text version is generated automatically.")}
                  </p>
                </div>
              </>
            )}
            <div>
              <label className="inline-flex cursor-pointer items-center gap-2 text-sm font-medium text-primary">
                <Paperclip size={15} /> {phrase("Attach files")}
                <input
                  type="file"
                  multiple
                  className="sr-only"
                  accept=".pdf,.png,.jpg,.jpeg,.txt"
                  onChange={(e) => void addFiles(e.target.files)}
                />
              </label>
              <ul className="mt-2 flex flex-wrap gap-2">
                {attachments.map((a, i) => (
                  <li
                    key={a.filename + i}
                    className="flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs"
                  >
                    {a.filename} ({Math.ceil(a.size / 1024)} KB)
                    <button
                      type="button"
                      aria-label={`Remove ${a.filename}`}
                      onClick={() => setAttachments(attachments.filter((_, j) => j !== i))}
                    >
                      <X size={12} />
                    </button>
                  </li>
                ))}
              </ul>
            </div>
            <Field
              label={phrase("Send later (optional)")}
              hint={phrase("Up to 7 days ahead, in your time zone.")}
            >
              <input
                type="datetime-local"
                className={inputClass}
                value={sendAt}
                onChange={(e) => setSendAt(e.target.value)}
              />
            </Field>
            <div className="flex flex-wrap items-center gap-3 border-t pt-4">
              <Button
                type="submit"
                disabled={busy}
                variant={env === "live" ? "destructive" : "default"}
              >
                {busy
                  ? phrase("Sending…")
                  : sendAt
                    ? phrase("Schedule")
                    : env === "sandbox"
                      ? phrase("Send to sandbox")
                      : phrase("Send")}
              </Button>
              {draftId && (
                <span className="text-xs text-muted-foreground">{phrase("Draft saved")}</span>
              )}
              {info && !info.environment_enabled && (
                <span className="text-xs text-destructive">
                  {phrase("This environment is disabled.")}
                </span>
              )}
            </div>
          </form>
        )}
      </SheetContent>
    </Sheet>
  );
}
