import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { admin } from "@/lib/admin-api";
import { Link } from "react-router-dom";
import { useWorkspace } from "@/features/workspace/context";
import { usePhrase } from "@/features/i18n/i18n";
export default function Settings() {
  const { to } = useWorkspace();
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["settings"],
    queryFn: () => admin.get<Record<string, number>>("/settings"),
  });
  const [values, setValues] = useState<Record<string, number>>({});
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (q.data) setValues(q.data);
  }, [q.data]);
  const save = async () => {
    setError("");
    setSaved(false);
    try {
      await admin.send("/settings", "PUT", values);
      setSaved(true);
      client.invalidateQueries({ queryKey: ["settings"] });
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    }
  };
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">{phrase("Settings")}</h1>
      <p className="text-muted-foreground">{phrase("Operator limits and retention settings.")}</p>
      <section className="space-y-3 rounded-xl border bg-card p-5">
        <h2 className="text-lg font-semibold">{phrase("Shared access")}</h2>
        <p className="text-sm text-muted-foreground">
          {phrase("Every enabled user is an admin and can access all pages and settings.")}
        </p>
        <nav
          aria-label={phrase("Administration settings")}
          className="flex flex-wrap gap-4 text-sm"
        >
          <Link className="text-primary underline" to={to("users")}>
            {phrase("Users")}
          </Link>
          <Link className="text-primary underline" to={to("mail-server")}>
            {phrase("Mail server")}
          </Link>
          <Link className="text-primary underline" to={to("privacy")}>
            {phrase("Recipient privacy")}
          </Link>
        </nav>
      </section>
      {q.isPending ? (
        <p role="status">{phrase("Loading settings…")}</p>
      ) : q.error ? (
        <p role="alert">{q.error.message}</p>
      ) : (
        <div className="grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-2">
          {Object.entries(values).map(([key, value]) => (
            <label key={key} className="grid gap-2 text-sm capitalize">
              {phrase(key.replaceAll("_", " "))}
              <input
                type="number"
                min="0"
                value={value}
                onChange={(e) => setValues({ ...values, [key]: Number(e.target.value) })}
                className="rounded border bg-background p-2"
              />
            </label>
          ))}
          {error && (
            <p role="alert" className="text-destructive sm:col-span-2">
              {error}
            </p>
          )}
          {saved && (
            <p role="status" className="sm:col-span-2">
              {phrase("Settings saved.")}
            </p>
          )}
          <button
            onClick={save}
            className="rounded bg-primary px-4 py-2 text-primary-foreground sm:col-span-2"
          >
            {phrase("Save settings")}
          </button>
        </div>
      )}
    </div>
  );
}
