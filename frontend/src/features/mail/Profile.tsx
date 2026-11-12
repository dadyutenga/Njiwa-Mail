import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { admin } from "@/lib/admin-api";
import { useAuth } from "@/features/auth/auth";
import { usePhrase } from "@/features/i18n/i18n";
import { TwoStep } from "./TwoStep";
type Session = {
  id: string;
  created_at: string;
  expires_at: string;
  user_agent: string;
  ip: string;
};
export default function Profile() {
  const { user, refresh } = useAuth();
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["sessions"],
    queryFn: () => admin.get<{ items: Session[] }>("/auth/sessions"),
  });
  const [error, setError] = useState("");
  const revoke = async (id: string) => {
    if (!window.confirm(phrase("Revoke this session?"))) return;
    try {
      await admin.send(`/auth/sessions/${encodeURIComponent(id)}`, "DELETE");
      client.invalidateQueries({ queryKey: ["sessions"] });
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    }
  };
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">{phrase("Profile")}</h1>
      <section className="rounded-xl border bg-card p-5">
        <p>
          <strong>{phrase("Email:")}</strong> {user?.email}
        </p>
        <p>
          <strong>{phrase("Role:")}</strong> {user?.role}
        </p>
      </section>
      <TwoStep enabled={!!user?.mfa_enabled} onChanged={refresh} />
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      <section className="rounded-xl border bg-card p-5">
        <h2 className="text-lg font-semibold">{phrase("Sessions")}</h2>
        {q.isPending ? (
          <p role="status">{phrase("Loading…")}</p>
        ) : q.error ? (
          <p role="alert">{q.error.message}</p>
        ) : (
          <ul className="mt-3 space-y-2">
            {q.data.items.map((s) => (
              <li
                key={s.id}
                className="flex flex-wrap items-center justify-between gap-3 border-t py-2 text-sm"
              >
                <span>
                  {s.user_agent || phrase("Browser")} · {s.ip} ·{" "}
                  {new Date(s.created_at).toLocaleString()}
                </span>
                <button className="text-primary underline" onClick={() => revoke(s.id)}>
                  {phrase("Revoke")}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
