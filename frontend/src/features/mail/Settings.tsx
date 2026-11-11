import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { admin } from "@/lib/admin-api";
import { useAuth } from "@/features/auth/auth";
import { usePhrase } from "@/features/i18n/i18n";
export default function Settings() {
  const { user } = useAuth();
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
            disabled={!user}
            className="rounded bg-primary px-4 py-2 text-primary-foreground sm:col-span-2"
          >
            {phrase("Save settings")}
          </button>
        </div>
      )}
    </div>
  );
}
