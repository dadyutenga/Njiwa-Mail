import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import {
  ConfirmButton,
  DataState,
  Empty,
  emailOk,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  textareaClass,
  Time,
  useParam,
  type Page,
  type Row,
} from "../ui";
import { cn } from "@/lib/utils";

type VarSpec = {
  name: string;
  type: "string" | "int" | "url";
  required?: boolean;
  max_len?: number;
  min?: number;
  max?: number;
  multiline?: boolean;
};
type Content = { subject: string; html: string; text: string };
type Version = {
  version: number;
  published_at: string | null;
  created_at: string;
  variable_schema: VarSpec[] | null;
  content: Record<string, Content>;
};
type Template = {
  id: string;
  slug: string;
  description: string;
  project: string | null;
  archived: boolean;
  versions: Version[];
};

const empty: Content = { subject: "", html: "", text: "" };

/** Variables used in content: {{.name}} (Go template syntax). */
function usedVars(c: Record<string, Content>) {
  const found = new Set<string>();
  for (const v of Object.values(c))
    for (const part of [v.subject, v.html, v.text])
      for (const m of part.matchAll(/\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g)) found.add(m[1]);
  return [...found];
}

export function Templates() {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const [id, setId] = useParam("id");
  const [creating, setCreating] = useState(false);
  const list = useQuery({
    queryKey: key("templates"),
    queryFn: () => api.get<Page>("/templates?limit=100"),
  });
  useEffect(() => {
    if (!id && list.data?.items.length)
      setId(s(list.data.items.find((t) => !t.archived)?.id ?? list.data.items[0].id));
  }, [id, list.data, setId]);
  return (
    <>
      <PageHeader
        title={phrase("Templates")}
        description={phrase(
          "Reusable messages with variables, in English and Kiswahili. Published versions never change; usable from Compose, Bulk send and the API by slug.",
        )}
        action={
          <Button onClick={() => setCreating(true)}>
            <Plus size={16} /> {phrase("New template")}
          </Button>
        }
      />
      <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
        <DataState
          q={list}
          isEmpty={(d) => d.items.length === 0}
          empty={
            <Empty
              title={phrase("No templates")}
              action={<Button onClick={() => setCreating(true)}>{phrase("New template")}</Button>}
            />
          }
        >
          {(d) => (
            <ul className="space-y-1 rounded-xl border bg-card p-2">
              {d.items.map((t) => (
                <li key={s(t.id)}>
                  <button
                    onClick={() => setId(s(t.id))}
                    aria-current={id === s(t.id)}
                    className={cn(
                      "w-full rounded-lg px-3 py-2 text-left text-sm hover:bg-muted",
                      id === s(t.id) && "bg-primary/15 font-semibold text-primary",
                      !!t.archived && "opacity-60",
                    )}
                  >
                    <span className="block truncate">{s(t.slug)}</span>
                    <span className="text-xs font-normal text-muted-foreground">
                      {t.published_version ? `v${s(t.published_version)}` : phrase("draft only")}
                      {t.draft_version ? ` · ${phrase("draft")} v${s(t.draft_version)}` : ""}
                      {t.shared ? ` · ${phrase("shared")}` : ""}
                      {t.archived ? ` · ${phrase("archived")}` : ""}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </DataState>
        <div>{id ? <Editor key={id} id={id} /> : null}</div>
      </div>
      <NewTemplateDialog open={creating} onOpenChange={setCreating} onCreated={setId} />
    </>
  );
}

function NewTemplateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (id: string) => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const valid = /^[a-z0-9][a-z0-9-]{0,62}$/.test(slug);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("New template")}</DialogTitle>
        <DialogDescription>
          {phrase("The slug is how applications choose this template.")}
        </DialogDescription>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!valid) return setError(phrase("Lowercase letters, digits and dashes."));
            try {
              const r = await api.send<{ id: string }>("/templates", "POST", { slug, description });
              await api.send(`/templates/${r.id}/draft`, "PUT", {
                variable_schema: [{ name: "name", type: "string", required: true, max_len: 100 }],
                content: {
                  en: {
                    subject: "Hello {{.name}}",
                    text: "Hello {{.name}},\n\n",
                    html: "<p>Hello {{.name}},</p>",
                  },
                },
              });
              client.invalidateQueries({ queryKey: key("templates") });
              onCreated(r.id);
              onOpenChange(false);
              setSlug("");
            } catch (err) {
              setError(errorMessage(err));
            }
          }}
        >
          <Field label={phrase("Slug")} error={error}>
            <input
              autoFocus
              className={inputClass}
              value={slug}
              onChange={(e) => setSlug(e.target.value.toLowerCase())}
              placeholder="welcome"
            />
          </Field>
          <Field label={phrase("Description")}>
            <input
              className={inputClass}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <Button type="submit" disabled={!valid}>
            {phrase("Create")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Editor({ id }: { id: string }) {
  const phrase = usePhrase();
  const { api, to, env } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: key("template", id),
    queryFn: () => api.get<Template>(`/templates/${id}`),
  });
  const senders = useQuery({
    queryKey: key("senders"),
    queryFn: () => api.get<Page>("/senders?limit=100"),
  });
  const t = q.data;
  const draft = t?.versions.find((v) => !v.published_at);
  const latest = draft ?? t?.versions[0];
  const shared = t ? t.project === null : false;
  const [locale, setLocale] = useState<"en" | "sw">("en");
  const [content, setContent] = useState<Record<string, Content>>({ en: { ...empty } });
  const [schema, setSchema] = useState<VarSpec[]>([]);
  const [sample, setSample] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<Content | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [testTo, setTestTo] = useState("");
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!latest) return;
    setContent({ en: { ...empty }, ...latest.content });
    setSchema(latest.variable_schema ?? []);
    setDirty(false);
  }, [latest]);
  const used = useMemo(() => usedVars(content), [content]);
  const declared = new Set(schema.map((v) => v.name));
  const undeclared = used.filter((v) => !declared.has(v));
  const missingSample = schema.filter((v) => v.required && !sample[v.name]).map((v) => v.name);
  const versionForPreview = draft?.version ?? latest?.version;

  // Live preview of the saved version with the sample values.
  useEffect(() => {
    if (!versionForPreview) return;
    const timer = setTimeout(async () => {
      const vars: Record<string, unknown> = {};
      for (const v of schema)
        if (sample[v.name])
          vars[v.name] = v.type === "int" ? Number(sample[v.name]) : sample[v.name];
      try {
        setPreview(
          await api.send<Content>(
            `/templates/${id}/versions/${versionForPreview}/preview`,
            "POST",
            { locale, variables: vars },
          ),
        );
        setPreviewError("");
      } catch (e) {
        setPreview(null);
        setPreviewError(errorMessage(e));
      }
    }, 400);
    return () => clearTimeout(timer);
  }, [api, id, versionForPreview, locale, sample, schema, q.dataUpdatedAt]);

  const update = (field: keyof Content, value: string) => {
    setContent({ ...content, [locale]: { ...(content[locale] ?? empty), [field]: value } });
    setDirty(true);
  };
  const saveDraft = async () => {
    setBusy(true);
    const c: Record<string, Content> = { en: content.en };
    if (content.sw && (content.sw.subject || content.sw.text || content.sw.html)) c.sw = content.sw;
    const ok = await run(
      () => api.send(`/templates/${id}/draft`, "PUT", { variable_schema: schema, content: c }),
      phrase("Draft saved."),
    );
    setBusy(false);
    if (ok) {
      setDirty(false);
      client.invalidateQueries({ queryKey: key("template", id) });
      client.invalidateQueries({ queryKey: key("templates") });
    }
  };
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("template", id) });
    client.invalidateQueries({ queryKey: key("templates") });
  };
  const verified = (senders.data?.items ?? []).filter((x) => x.status === "verified");
  return (
    <DataState q={q}>
      {(tpl) => (
        <div className="space-y-4">
          <Section
            title={
              <span className="flex flex-wrap items-center gap-2">
                {tpl.slug}{" "}
                {tpl.archived && <StateBadge state="cancelled" label={phrase("archived")} />}
                {shared && <StateBadge state="queued" label={phrase("shared")} />}
              </span>
            }
            action={
              <div className="flex flex-wrap gap-2">
                <Link
                  className="text-sm text-primary"
                  to={to(`messages?template=${encodeURIComponent(tpl.slug)}`)}
                >
                  {phrase("Messages sent with it")}
                </Link>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={async () => {
                    const slug = window.prompt(phrase("Slug for the copy"), `${tpl.slug}-copy`);
                    if (!slug) return;
                    const r = await run(
                      () =>
                        api.send<{ id: string }>(`/templates/${id}/duplicate`, "POST", { slug }),
                      phrase("Template duplicated."),
                    );
                    if (r) client.invalidateQueries({ queryKey: key("templates") });
                  }}
                >
                  {phrase("Duplicate")}
                </Button>
                {!shared && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={async () => {
                      await run(
                        () => api.send(`/templates/${id}`, "PATCH", { archived: !tpl.archived }),
                        tpl.archived ? phrase("Restored.") : phrase("Archived."),
                      );
                      refresh();
                    }}
                  >
                    {tpl.archived ? phrase("Restore") : phrase("Archive")}
                  </Button>
                )}
              </div>
            }
          >
            {shared && (
              <p className="mb-3 text-sm text-muted-foreground">
                {phrase(
                  "Shared templates are read-only here. Duplicate it to change it for this project.",
                )}
              </p>
            )}
            <div
              className="mb-3 flex gap-2 border-b"
              role="tablist"
              aria-label={phrase("Language")}
            >
              {(["en", "sw"] as const).map((l) => (
                <button
                  key={l}
                  role="tab"
                  aria-selected={locale === l}
                  onClick={() => setLocale(l)}
                  className={`-mb-px border-b-2 px-3 py-2 text-sm ${locale === l ? "border-primary font-semibold" : "border-transparent text-muted-foreground"}`}
                >
                  {l === "en" ? "English" : "Kiswahili"}
                </button>
              ))}
            </div>
            <fieldset disabled={shared} className="grid gap-3">
              <Field label={phrase("Subject")}>
                <input
                  className={inputClass}
                  value={content[locale]?.subject ?? ""}
                  onChange={(e) => update("subject", e.target.value)}
                />
              </Field>
              <Field
                label={phrase("Plain-text body")}
                hint={phrase("Insert variables as {{.name}}.")}
              >
                <textarea
                  className={textareaClass}
                  rows={6}
                  value={content[locale]?.text ?? ""}
                  onChange={(e) => update("text", e.target.value)}
                />
              </Field>
              <Field label={phrase("HTML body")}>
                <textarea
                  className={textareaClass + " font-mono"}
                  rows={6}
                  value={content[locale]?.html ?? ""}
                  onChange={(e) => update("html", e.target.value)}
                />
              </Field>
              {locale === "sw" && !content.sw?.subject && (
                <p className="text-xs text-muted-foreground">
                  {phrase("Without Kiswahili content, Kiswahili requests fall back to English.")}
                </p>
              )}
              <div>
                <p className="mb-2 text-sm font-medium">{phrase("Variables")}</p>
                <ul className="space-y-2">
                  {schema.map((v, i) => (
                    <li key={i} className="grid grid-cols-[1fr_7rem_auto_auto] items-center gap-2">
                      <input
                        aria-label={phrase("Variable name")}
                        className={inputClass}
                        value={v.name}
                        onChange={(e) => {
                          const next = [...schema];
                          next[i] = { ...v, name: e.target.value.replace(/[^A-Za-z0-9_]/g, "") };
                          setSchema(next);
                          setDirty(true);
                        }}
                      />
                      <select
                        aria-label={phrase("Type")}
                        className={selectClass}
                        value={v.type}
                        onChange={(e) => {
                          const next = [...schema];
                          const type = e.target.value as VarSpec["type"];
                          next[i] =
                            type === "int"
                              ? { name: v.name, type, required: v.required, min: 0, max: 1000000 }
                              : {
                                  name: v.name,
                                  type,
                                  required: v.required,
                                  max_len: type === "url" ? 2048 : 200,
                                };
                          setSchema(next);
                          setDirty(true);
                        }}
                      >
                        <option value="string">text</option>
                        <option value="int">number</option>
                        <option value="url">link</option>
                      </select>
                      <label className="flex items-center gap-1 text-xs">
                        <input
                          type="checkbox"
                          checked={!!v.required}
                          onChange={(e) => {
                            const next = [...schema];
                            next[i] = { ...v, required: e.target.checked };
                            setSchema(next);
                            setDirty(true);
                          }}
                        />
                        {phrase("required")}
                      </label>
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        aria-label={`${phrase("Remove")} ${v.name}`}
                        onClick={() => {
                          setSchema(schema.filter((_, j) => j !== i));
                          setDirty(true);
                        }}
                      >
                        <Trash2 size={14} />
                      </Button>
                    </li>
                  ))}
                </ul>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="mt-2"
                  onClick={() => {
                    setSchema([
                      ...schema,
                      { name: undeclared[0] ?? "", type: "string", required: true, max_len: 200 },
                    ]);
                    setDirty(true);
                  }}
                >
                  <Plus size={14} /> {phrase("Add variable")}
                </Button>
                {undeclared.length > 0 && (
                  <p role="alert" className="mt-2 text-xs text-destructive">
                    {phrase("Used but not declared:")} {undeclared.join(", ")}
                  </p>
                )}
                <p className="mt-2 text-xs text-muted-foreground">
                  {phrase(
                    "Links (url type) must start with one of this environment's approved origins.",
                  )}
                </p>
              </div>
            </fieldset>
            {!shared && (
              <div className="mt-4 flex flex-wrap items-center gap-2 border-t pt-4">
                <Button onClick={saveDraft} disabled={busy || undeclared.length > 0}>
                  {phrase("Save draft")}
                </Button>
                {draft && !dirty && (
                  <ConfirmButton
                    size="default"
                    destructive={false}
                    title={`${phrase("Publish")} v${draft.version}?`}
                    description={phrase(
                      "Published versions can never be changed. Applications that do not pin a version start using it at once.",
                    )}
                    confirm="Publish"
                    onConfirm={async () => {
                      await run(
                        () =>
                          api.send(`/templates/${id}/versions/${draft.version}/publish`, "POST"),
                        `${phrase("Published version")} ${draft.version}.`,
                      );
                      refresh();
                    }}
                  >
                    {phrase("Publish")} v{draft.version}
                  </ConfirmButton>
                )}
                {dirty && (
                  <span className="text-xs text-warning">
                    {phrase("Unsaved changes. Preview shows the saved version.")}
                  </span>
                )}
              </div>
            )}
          </Section>
          <div className="grid gap-4 xl:grid-cols-2">
            <Section title={phrase("Preview")}>
              <div className="mb-3 grid gap-2 sm:grid-cols-2">
                {schema.map((v) => (
                  <Field key={v.name} label={`${v.name}${v.required ? " *" : ""}`}>
                    <input
                      className={inputClass}
                      value={sample[v.name] ?? ""}
                      onChange={(e) => setSample({ ...sample, [v.name]: e.target.value })}
                    />
                  </Field>
                ))}
              </div>
              {missingSample.length > 0 && (
                <p className="mb-2 text-xs text-warning">
                  {phrase("Missing sample values:")} {missingSample.join(", ")}
                </p>
              )}
              {previewError ? (
                <p className="text-sm text-destructive">{previewError}</p>
              ) : preview ? (
                <div className="space-y-2">
                  <p className="text-sm font-semibold">{preview.subject}</p>
                  {preview.html ? (
                    <iframe
                      title={phrase("HTML preview")}
                      sandbox=""
                      srcDoc={preview.html}
                      className="h-64 w-full rounded border bg-white"
                    />
                  ) : null}
                  <pre className="whitespace-pre-wrap rounded bg-muted/40 p-3 font-sans text-xs">
                    {preview.text}
                  </pre>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">
                  {phrase("Save a draft to preview it.")}
                </p>
              )}
            </Section>
            <Section title={phrase("Send a test")}>
              <p className="mb-3 text-sm text-muted-foreground">
                {env === "sandbox"
                  ? phrase(
                      "In Sandbox the test is captured, never delivered; drafts can be tested.",
                    )
                  : phrase(
                      "In Live only a published version can be tested, and only to an address on this environment's approved list.",
                    )}
              </p>
              <div className="flex flex-wrap items-end gap-2">
                <Field label={phrase("To")} className="flex-1">
                  <input
                    className={inputClass}
                    type="email"
                    value={testTo}
                    onChange={(e) => setTestTo(e.target.value)}
                  />
                </Field>
                <Button
                  disabled={!emailOk(testTo) || !verified.length || missingSample.length > 0}
                  onClick={async () => {
                    const v =
                      env === "live"
                        ? tpl.versions.find((x) => x.published_at)?.version
                        : versionForPreview;
                    if (!v) return;
                    const vars: Record<string, unknown> = {};
                    for (const x of schema)
                      if (sample[x.name])
                        vars[x.name] = x.type === "int" ? Number(sample[x.name]) : sample[x.name];
                    const r = await run(
                      () =>
                        api.send<{ id: string }>(
                          `/templates/${id}/versions/${v}/test-send`,
                          "POST",
                          {
                            from: s(verified.find((x) => x.is_default)?.email ?? verified[0].email),
                            to: testTo,
                            locale,
                            variables: vars,
                          },
                        ),
                      env === "sandbox"
                        ? phrase("Test captured in the Sandbox.")
                        : phrase("Test queued."),
                    );
                    if (r) client.invalidateQueries({ queryKey: key("messages") });
                  }}
                >
                  {phrase("Send test")}
                </Button>
              </div>
              {!verified.length && (
                <p className="mt-2 text-xs">
                  <Link className="text-primary" to={to("senders")}>
                    {phrase("Add a verified sender first")}
                  </Link>
                </p>
              )}
            </Section>
          </div>
          <Section title={phrase("Version history")}>
            <ul className="divide-y text-sm">
              {tpl.versions.map((v) => (
                <li key={v.version} className="flex flex-wrap items-center gap-2 py-2">
                  <span className="font-medium">v{v.version}</span>
                  <StateBadge
                    state={v.published_at ? "verified" : "draft"}
                    label={v.published_at ? phrase("published") : phrase("draft")}
                  />
                  <span className="text-muted-foreground">
                    <Time value={v.published_at ?? v.created_at} />
                  </span>
                  <Link
                    className="ml-auto text-primary"
                    to={to(`messages?template=${encodeURIComponent(tpl.slug)}`)}
                  >
                    {phrase("messages")}
                  </Link>
                </li>
              ))}
            </ul>
          </Section>
        </div>
      )}
    </DataState>
  );
}
