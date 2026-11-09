import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Banner,
  controlClass,
  errText,
  Field,
  getJSON,
  Modal,
  PageTitle,
  send,
  ServerTable,
  show,
  useCan,
  useDraft,
  useList,
  type Row,
} from "@/components/admin/bits";

interface LocaleContent {
  subject: string;
  html: string;
  text: string;
}

interface Version {
  version: number;
  published_at?: string | null;
  variable_schema: unknown;
  content: Record<string, LocaleContent>;
}

const emptyLocale = (): LocaleContent => ({ subject: "", html: "", text: "" });

export function TemplatesPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({});
  const query = useList("/templates", { offset: filters.offset });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ slug: "", description: "", project: "" });
  const create = useMutation({
    mutationFn: () => send("post", "/templates", { slug: form.slug, description: form.description, project: form.project }),
    onSuccess: () => {
      toast.success("Template created.");
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Templates" description="Published versions are immutable. English is required and is the fallback when Swahili is missing.">
        {canWrite && <Button onClick={() => setOpen(true)}>New template</Button>}
      </PageTitle>
      <ServerTable
        columns={[
          { header: "Slug", cell: (row) => show(row.slug) },
          { header: "Project", cell: (row) => show(row.project) },
          { header: "Published", cell: (row) => show(row.published_version) },
          { header: "Draft", cell: (row) => show(row.draft_version) },
          { header: "", cell: (row) => <Button variant="link" size="sm" asChild><Link to={`/templates/${show(row.id)}`}>Open</Link></Button> },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No templates."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={open} title="New template" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Create</Button>}>
        <div className="grid gap-3">
          <Field label="Slug"><Input value={form.slug} onChange={(e) => setForm({ ...form, slug: e.target.value })} placeholder="otp" /></Field>
          <Field label="Description"><Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
          <Field label="Project (empty = shared)"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
        </div>
      </Modal>
    </div>
  );
}

export function TemplatePage() {
  const { id = "" } = useParams();
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["admin", "template", id], queryFn: () => getJSON<Row>(`/templates/${id}`), enabled: Boolean(id) });
  const versions = useMemo(() => (query.data?.versions as Version[] | undefined) ?? [], [query.data]);
  const [version, setVersion] = useState<number | null>(null);
  const [schema, setSchema] = useState("[]");
  const [en, setEn] = useState<LocaleContent>(emptyLocale());
  const [sw, setSw] = useState<LocaleContent>(emptyLocale());
  const [preview, setPreview] = useState<{ subject: string; html: string; text: string } | null>(null);
  const [locale, setLocale] = useState("en");
  const [variables, setVariables] = useState("{}");
  const [sendForm, setSendForm] = useState({ project: "", mode: "sandbox", from: "", to: "" });

  useEffect(() => {
    if (versions.length === 0 || version !== null) return;
    const draft = versions.find((item) => !item.published_at) ?? versions[0];
    load(draft);
  }, [versions, version]);

  function load(item: Version) {
    setVersion(item.version);
    setSchema(JSON.stringify(item.variable_schema ?? [], null, 2));
    setEn({ ...emptyLocale(), ...item.content?.en });
    setSw({ ...emptyLocale(), ...item.content?.sw });
    setPreview(null);
  }

  const selected = versions.find((item) => item.version === version);
  const published = Boolean(selected?.published_at);

  const save = useMutation({
    mutationFn: () => {
      const content: Record<string, LocaleContent> = { en };
      if (sw.subject || sw.html || sw.text) content.sw = sw;
      return send("put", `/templates/${id}/draft`, { variable_schema: JSON.parse(schema), content });
    },
    onSuccess: () => {
      toast.success("Draft saved.");
      setVersion(null);
      void queryClient.invalidateQueries({ queryKey: ["admin", "template", id] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const publish = useMutation({
    mutationFn: () => send("post", `/templates/${id}/versions/${version}/publish`),
    onSuccess: () => {
      toast.success("Published. This version can no longer be edited.");
      setVersion(null);
      void queryClient.invalidateQueries({ queryKey: ["admin", "template", id] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const previewCall = useMutation({
    mutationFn: () => send("post", `/templates/${id}/versions/${version}/preview`, {
      locale,
      variables: JSON.parse(variables),
    }) as Promise<{ subject: string; html: string; text: string }>,
    onSuccess: (data) => setPreview(data),
    onError: (error) => toast.error(errText(error)),
  });
  const testSend = useMutation({
    mutationFn: () => send("post", `/templates/${id}/versions/${version}/test-send`, {
      ...sendForm,
      locale,
      variables: JSON.parse(variables),
    }),
    onSuccess: () => toast.success(sendForm.mode === "sandbox" ? "Captured in sandbox." : "Queued."),
    onError: (error) => toast.error(errText(error)),
  });

  return (
    <div className="space-y-4">
      <PageTitle title={show(query.data?.slug)} description={show(query.data?.description)} />
      <Banner error={query.error} />
      <div className="flex flex-wrap gap-2">
        {versions.map((item) => (
          <Button key={item.version} size="sm" variant={item.version === version ? "default" : "outline"} onClick={() => load(item)}>
            v{item.version} {item.published_at ? "published" : "draft"}
          </Button>
        ))}
      </div>
      <Field label="Variable schema">
        <Textarea className="min-h-28 font-mono text-xs" value={schema} onChange={(e) => setSchema(e.target.value)} readOnly={published || !canWrite} />
      </Field>
      <Tabs defaultValue="en">
        <TabsList>
          <TabsTrigger value="en">English</TabsTrigger>
          <TabsTrigger value="sw">Kiswahili</TabsTrigger>
        </TabsList>
        <LocaleEditor value={en} onChange={setEn} readOnly={published || !canWrite} tab="en" />
        <LocaleEditor value={sw} onChange={setSw} readOnly={published || !canWrite} tab="sw" />
      </Tabs>
      {canWrite && (
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => save.mutate()} disabled={save.isPending}>Save draft</Button>
          {!published && version !== null && <Button variant="secondary" onClick={() => publish.mutate()} disabled={publish.isPending}>Publish</Button>}
        </div>
      )}
      <div className="grid gap-3 rounded-xl border p-4">
        <h2 className="font-semibold">Preview and test send</h2>
        <p className="text-sm text-muted-foreground">Preview never sends. A live test send only goes to an address on that environment's allowlist.</p>
        <div className="flex flex-wrap gap-3">
          <Field label="Locale">
            <select className={controlClass} value={locale} onChange={(e) => setLocale(e.target.value)}>
              <option value="en">en</option>
              <option value="sw">sw</option>
            </select>
          </Field>
          <Field label="Sample variables"><Textarea className="font-mono text-xs" value={variables} onChange={(e) => setVariables(e.target.value)} /></Field>
        </div>
        <Button variant="outline" onClick={() => previewCall.mutate()} disabled={version === null || previewCall.isPending}>Preview saved version</Button>
        {preview && (
          <div className="space-y-2">
            <p className="text-sm font-medium">{preview.subject}</p>
            <pre className="whitespace-pre-wrap rounded-md border p-3 text-xs">{preview.text}</pre>
            <iframe title="Template preview" sandbox="" className="h-64 w-full rounded-md border bg-white" srcDoc={preview.html} />
          </div>
        )}
        {canWrite && (
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="Project"><Input value={sendForm.project} onChange={(e) => setSendForm({ ...sendForm, project: e.target.value })} /></Field>
            <Field label="Mode">
              <select className={controlClass} value={sendForm.mode} onChange={(e) => setSendForm({ ...sendForm, mode: e.target.value })}>
                <option value="sandbox">sandbox</option>
                <option value="live">live</option>
              </select>
            </Field>
            <Field label="From"><Input value={sendForm.from} onChange={(e) => setSendForm({ ...sendForm, from: e.target.value })} /></Field>
            <Field label="To"><Input value={sendForm.to} onChange={(e) => setSendForm({ ...sendForm, to: e.target.value })} /></Field>
            <Button onClick={() => testSend.mutate()} disabled={testSend.isPending || version === null}>Test send</Button>
          </div>
        )}
      </div>
    </div>
  );
}

function LocaleEditor({
  value,
  onChange,
  readOnly,
  tab,
}: {
  value: LocaleContent;
  onChange: (value: LocaleContent) => void;
  readOnly: boolean;
  tab: string;
}) {
  return (
    <TabsContent value={tab} className="grid gap-3">
      <Field label="Subject"><Input value={value.subject} readOnly={readOnly} onChange={(e) => onChange({ ...value, subject: e.target.value })} /></Field>
      <Field label="HTML"><Textarea className="min-h-40 font-mono text-xs" value={value.html} readOnly={readOnly} onChange={(e) => onChange({ ...value, html: e.target.value })} /></Field>
      <Field label="Text"><Textarea className="min-h-28 font-mono text-xs" value={value.text} readOnly={readOnly} onChange={(e) => onChange({ ...value, text: e.target.value })} /></Field>
    </TabsContent>
  );
}

export default TemplatesPage;
