import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { admin } from "@/lib/admin-api";
type Version = {
  version: number;
  variable_schema: unknown;
  content: Record<string, { subject: string; html: string; text: string }>;
  published_at: string | null;
};
type Template = {
  id: string;
  slug: string;
  description: string;
  project: string | null;
  versions: Version[];
};
type Preview = { locale: string; subject: string; text: string; html: string };
export default function TemplateDetail() {
  const { id } = useParams();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["template", id],
    queryFn: () => admin.get<Template>(`/templates/${encodeURIComponent(id || "")}`),
    enabled: !!id,
  });
  const [schema, setSchema] = useState("[]");
  const [content, setContent] = useState('{"en":{"subject":"","html":"","text":""}}');
  const [vars, setVars] = useState("{}");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [testProject, setTestProject] = useState("");
  const [testFrom, setTestFrom] = useState("");
  const [testTo, setTestTo] = useState("");
  const [testVersion, setTestVersion] = useState(0);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => {
    const draft = q.data?.versions.find((v) => !v.published_at);
    if (draft) {
      setSchema(JSON.stringify(draft.variable_schema, null, 2));
      setContent(JSON.stringify(draft.content, null, 2));
    }
  }, [q.data]);
  const run = async (fn: () => Promise<unknown>, success: string) => {
    setError("");
    setMessage("");
    try {
      await fn();
      setMessage(success);
      client.invalidateQueries({ queryKey: ["template", id] });
      client.invalidateQueries({ queryKey: ["resource", "templates"] });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Request failed");
    }
  };
  if (q.isPending) return <p role="status">Loading template…</p>;
  if (q.error) return <p role="alert">{q.error.message}</p>;
  const t = q.data;
  return (
    <div className="space-y-6">
      <Link to="/templates" className="text-primary">
        ← Templates
      </Link>
      <h1 className="text-3xl font-bold">{t.slug}</h1>
      <p className="text-muted-foreground">
        {t.description} · {t.project || "Shared template"}
      </p>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      {message && (
        <p role="status" className="text-primary">
          {message}
        </p>
      )}
      <section className="space-y-4 rounded-xl border bg-card p-5">
        <h2 className="text-xl font-semibold">Draft editor</h2>
        <p className="text-sm text-muted-foreground">
          Edit JSON for the variable schema and locale content. Save before publishing.
        </p>
        <label className="block text-sm">
          Variable schema
          <textarea
            value={schema}
            onChange={(e) => setSchema(e.target.value)}
            rows={6}
            spellCheck={false}
            className="mt-1 w-full rounded border bg-background p-3 font-mono text-xs"
          />
        </label>
        <label className="block text-sm">
          Content by locale
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            rows={12}
            spellCheck={false}
            className="mt-1 w-full rounded border bg-background p-3 font-mono text-xs"
          />
        </label>
        <button
          className="rounded bg-primary px-4 py-2 text-primary-foreground"
          onClick={() =>
            run(
              () =>
                admin.send(`/templates/${id}/draft`, "PUT", {
                  variable_schema: JSON.parse(schema),
                  content: JSON.parse(content),
                }),
              "Draft saved",
            )
          }
        >
          Save draft
        </button>
      </section>
      <section className="space-y-4">
        <h2 className="text-xl font-semibold">Versions</h2>
        <label className="block text-sm">
          Preview variables (JSON)
          <textarea
            value={vars}
            onChange={(e) => setVars(e.target.value)}
            rows={3}
            className="mt-1 w-full rounded border bg-background p-3 font-mono text-xs"
          />
        </label>
        {t.versions.map((v) => (
          <div key={v.version} className="rounded-xl border bg-card p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <strong>
                Version {v.version} · {v.published_at ? "Published" : "Draft"}
              </strong>
              <div className="flex gap-2">
                <button
                  className="rounded border px-3 py-1 text-sm"
                  onClick={() =>
                    run(async () => {
                      const result = await admin.send<Preview>(
                        `/templates/${id}/versions/${v.version}/preview`,
                        "POST",
                        { locale: "en", variables: JSON.parse(vars) },
                      );
                      setPreview(result);
                    }, "Preview ready")
                  }
                >
                  Preview
                </button>
                {!v.published_at && (
                  <button
                    className="rounded bg-primary px-3 py-1 text-sm text-primary-foreground"
                    onClick={() => {
                      if (window.confirm("Publish this version?"))
                        run(
                          () =>
                            admin.send(`/templates/${id}/versions/${v.version}/publish`, "POST"),
                          "Version published",
                        );
                    }}
                  >
                    Publish
                  </button>
                )}
              </div>
            </div>
          </div>
        ))}
      </section>
      <section className="space-y-4 rounded-xl border bg-card p-5">
        <h2 className="text-xl font-semibold">Sandbox test send</h2>
        <p className="text-sm text-muted-foreground">
          This captures a message locally and does not contact a mail server. Use approved sender
          details and sample variables above.
        </p>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-sm">
            Version
            <select
              className="rounded border bg-background p-2"
              value={testVersion || t.versions[0]?.version || 0}
              onChange={(e) => setTestVersion(Number(e.target.value))}
            >
              {t.versions.map((v) => (
                <option key={v.version} value={v.version}>
                  {v.version}
                </option>
              ))}
            </select>
          </label>
          <label className="grid gap-1 text-sm">
            Project
            <input
              className="rounded border bg-background p-2"
              value={testProject}
              onChange={(e) => setTestProject(e.target.value)}
            />
          </label>
          <label className="grid gap-1 text-sm">
            Approved sender
            <input
              type="email"
              className="rounded border bg-background p-2"
              value={testFrom}
              onChange={(e) => setTestFrom(e.target.value)}
            />
          </label>
          <label className="grid gap-1 text-sm">
            Recipient
            <input
              type="email"
              className="rounded border bg-background p-2"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
            />
          </label>
        </div>
        <button
          disabled={!t.versions.length || !testProject || !testFrom || !testTo}
          className="rounded bg-primary px-4 py-2 text-primary-foreground disabled:opacity-50"
          onClick={() =>
            run(
              () =>
                admin.send(
                  `/templates/${id}/versions/${testVersion || t.versions[0].version}/test-send`,
                  "POST",
                  {
                    project: testProject,
                    mode: "sandbox",
                    from: testFrom,
                    to: testTo,
                    locale: "en",
                    variables: JSON.parse(vars),
                  },
                ),
              "Sandbox message queued",
            )
          }
        >
          Send to sandbox
        </button>
      </section>
      {preview && (
        <section className="rounded-xl border bg-card p-5">
          <h2 className="text-xl font-semibold">Preview: {preview.subject}</h2>
          <pre className="mt-3 whitespace-pre-wrap text-sm">{preview.text}</pre>
        </section>
      )}
    </div>
  );
}
