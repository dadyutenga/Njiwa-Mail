import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { admin, type Page } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
type Env = {
  mode: string;
  enabled: boolean;
  approved_origins: string[];
  restrict_recipients: boolean;
  recipient_allowlist: string[];
};
type Project = { name: string; enabled: boolean; environments: Env[] };
function Environment({ project, env }: { project: string; env: Env }) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [enabled, setEnabled] = useState(env.enabled);
  const [restrict, setRestrict] = useState(env.restrict_recipients);
  const [origins, setOrigins] = useState((env.approved_origins || []).join("\n"));
  const [allowlist, setAllowlist] = useState((env.recipient_allowlist || []).join("\n"));
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    setEnabled(env.enabled);
    setRestrict(env.restrict_recipients);
    setOrigins((env.approved_origins || []).join("\n"));
    setAllowlist((env.recipient_allowlist || []).join("\n"));
  }, [env]);
  const save = async () => {
    setError("");
    setSaved(false);
    try {
      await admin.send(
        `/projects/${encodeURIComponent(project)}/environments/${env.mode}`,
        "PATCH",
        {
          enabled,
          restrict_recipients: restrict,
          approved_origins: origins
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean),
          recipient_allowlist: allowlist
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean),
        },
      );
      setSaved(true);
      client.invalidateQueries({ queryKey: ["projects-detail"] });
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Save failed"));
    }
  };
  return (
    <section className="space-y-4 rounded-xl border bg-card p-5">
      <h2 className="text-xl font-semibold capitalize">{env.mode}</h2>
      <label className="flex gap-2">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        {phrase("Enabled")}
      </label>
      <label className="flex gap-2">
        <input type="checkbox" checked={restrict} onChange={(e) => setRestrict(e.target.checked)} />
        {phrase("Restrict recipients")}
      </label>
      <label className="grid gap-1 text-sm">
        {phrase("Approved origins, one per line")}
        <textarea
          className="rounded border bg-background p-2"
          rows={3}
          value={origins}
          onChange={(e) => setOrigins(e.target.value)}
        />
      </label>
      <label className="grid gap-1 text-sm">
        {phrase("Recipient allowlist, one email or @domain per line")}
        <textarea
          className="rounded border bg-background p-2"
          rows={3}
          value={allowlist}
          onChange={(e) => setAllowlist(e.target.value)}
        />
      </label>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      {saved && (
        <p role="status" className="text-primary">
          {phrase("Saved.")}
        </p>
      )}
      <button onClick={save} className="rounded bg-primary px-4 py-2 text-primary-foreground">
        {phrase("Save environment")}
      </button>
    </section>
  );
}
export default function ProjectDetail() {
  const phrase = usePhrase();
  const { name } = useParams();
  const q = useQuery({
    queryKey: ["projects-detail", name],
    queryFn: async () => {
      let offset = 0;
      while (true) {
        const page = await admin.get<Page<Project>>(`/projects?limit=100&offset=${offset}`);
        const match = page.items.find((p) => p.name === name);
        if (match) return match;
        if (!page.has_more) throw new Error(phrase("Project not found"));
        offset += 100;
      }
    },
  });
  if (q.isPending) return <p role="status">{phrase("Loading project…")}</p>;
  if (q.error) return <p role="alert">{q.error.message}</p>;
  return (
    <div className="space-y-6">
      <Link to="/projects" className="text-primary">
        {phrase("← Projects")}
      </Link>
      <h1 className="text-3xl font-bold">{q.data.name}</h1>
      <p className="text-muted-foreground">{phrase("Manage sandbox and live sending rules.")}</p>
      <div className="grid gap-5 lg:grid-cols-2">
        {q.data.environments?.map((e) => (
          <Environment key={e.mode} project={q.data.name} env={e} />
        ))}
      </div>
    </div>
  );
}
