import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { admin } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";

type Session = {
  id: string;
  created_at: string;
  expires_at: string;
  user_agent: string;
  ip: string;
};

/** Live sign-ins for the current account. Revoking one ends that browser only. */
export function Sessions() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["sessions"],
    queryFn: () => admin.get<{ items: Session[] }>("/auth/sessions"),
  });
  const [pending, setPending] = useState<Session | null>(null);
  const [error, setError] = useState("");
  const revoke = async () => {
    if (!pending) return;
    setError("");
    try {
      await admin.send(`/auth/sessions/${encodeURIComponent(pending.id)}`, "DELETE");
      client.invalidateQueries({ queryKey: ["sessions"] });
      setPending(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    }
  };
  return (
    <section className="mt-6 rounded-xl border bg-card p-5">
      <h2 className="text-lg font-semibold">{phrase("Sessions")}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {phrase(
          "Browsers that are signed in to this account. Revoking one signs that browser out.",
        )}
      </p>
      {error && !pending && (
        <p role="alert" className="mt-3 text-sm text-destructive">
          {error}
        </p>
      )}
      {q.isPending ? (
        <p role="status" className="mt-3">
          {phrase("Loading…")}
        </p>
      ) : q.error ? (
        <p role="alert" className="mt-3">
          {q.error.message}
        </p>
      ) : (
        <ul className="mt-3">
          {q.data.items.map((s) => (
            <li
              key={s.id}
              className="flex flex-wrap items-center justify-between gap-3 border-t py-3 text-sm"
            >
              <span>
                <span className="block">{s.user_agent || phrase("Browser")}</span>
                <span className="text-xs text-muted-foreground">
                  {s.ip} · {new Date(s.created_at).toLocaleString()}
                </span>
              </span>
              <button className="text-primary underline" onClick={() => setPending(s)}>
                {phrase("Revoke")}
              </button>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={!!pending} onOpenChange={(open) => !open && setPending(null)}>
        <DialogContent>
          <DialogTitle>{phrase("Revoke this session?")}</DialogTitle>
          <DialogDescription>
            {pending?.user_agent || phrase("Browser")}
            {pending ? ` · ${pending.ip}` : ""}
            {". "}
            {phrase(
              "That browser is signed out. This one stays signed in unless you revoke it too.",
            )}
          </DialogDescription>
          {error && pending && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button variant="destructive" onClick={revoke}>
              {phrase("Revoke")}
            </Button>
            <Button type="button" variant="outline" onClick={() => setPending(null)}>
              {phrase("Cancel")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </section>
  );
}
