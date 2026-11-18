import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Monitor, ShieldCheck, Smartphone } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { admin } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
import { cn } from "@/lib/utils";

type Session = {
  id: string;
  created_at: string;
  expires_at: string;
  user_agent: string;
  ip: string;
  current?: boolean;
};

type Device = { browser: string; os: string; kind: "desktop" | "mobile" | "other" };

/** A readable name for a User-Agent string: "Firefox on Linux". Unknown parts stay empty. */
function describeAgent(ua: string): Device {
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /OPR\/|Opera/.test(ua)
      ? "Opera"
      : /Firefox\//.test(ua)
        ? "Firefox"
        : /Chrome\//.test(ua)
          ? "Chrome"
          : /Safari\//.test(ua)
            ? "Safari"
            : /curl|python|Go-http|okhttp|axios|node/i.test(ua)
              ? "Script"
              : "";
  const os = /Android/.test(ua)
    ? "Android"
    : /iPhone|iPad/.test(ua)
      ? "iOS"
      : /Windows/.test(ua)
        ? "Windows"
        : /Mac OS X|Macintosh/.test(ua)
          ? "macOS"
          : /CrOS/.test(ua)
            ? "ChromeOS"
            : /Linux/.test(ua)
              ? "Linux"
              : "";
  const kind = /Mobile|Android|iPhone/.test(ua)
    ? "mobile"
    : browser && browser !== "Script"
      ? "desktop"
      : "other";
  return { browser, os, kind };
}

function ago(iso: string) {
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 1) return { n: 0, unit: "now" };
  if (mins < 60) return { n: mins, unit: "min" };
  if (mins < 1440) return { n: Math.round(mins / 60), unit: "h" };
  return { n: Math.round(mins / 1440), unit: "d" };
}

/** Live sign-ins for the current account. Revoking one ends that browser only. */
export function Sessions() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["sessions"],
    queryFn: () => admin.get<{ items: Session[] }>("/auth/sessions"),
  });
  const [pending, setPending] = useState<Session | "others" | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const revoke = async () => {
    if (!pending) return;
    setError("");
    setBusy(true);
    try {
      if (pending === "others") await admin.send("/auth/sessions/revoke-others", "POST", {});
      else await admin.send(`/auth/sessions/${encodeURIComponent(pending.id)}`, "DELETE");
      client.invalidateQueries({ queryKey: ["sessions"] });
      setPending(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    } finally {
      setBusy(false);
    }
  };
  const name = (s: Session) => {
    const d = describeAgent(s.user_agent);
    if (d.browser && d.os) return `${d.browser} ${phrase("on")} ${d.os}`;
    return d.browser || d.os || phrase("Unknown browser");
  };
  const when = (iso: string) => {
    const a = ago(iso);
    return a.unit === "now" ? phrase("just now") : `${a.n} ${phrase(a.unit)} ${phrase("ago")}`;
  };
  const daysLeft = (iso: string) =>
    Math.max(0, Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000));
  const items = q.data?.items ?? [];
  const others = items.filter((s) => !s.current).length;

  return (
    <section className="mt-6 overflow-hidden rounded-xl border bg-card">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b p-5">
        <div className="flex gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <ShieldCheck size={20} aria-hidden="true" />
          </span>
          <div>
            <h2 className="text-lg font-semibold">
              {phrase("Sessions")}
              {q.data && (
                <span className="ml-2 align-middle text-sm font-normal text-muted-foreground">
                  {items.length}
                </span>
              )}
            </h2>
            <p className="mt-0.5 text-sm text-muted-foreground">
              {phrase(
                "Browsers that are signed in to this account. Revoking one signs that browser out.",
              )}
            </p>
          </div>
        </div>
        {others > 0 && (
          <Button variant="outline" size="sm" onClick={() => setPending("others")}>
            {phrase("Sign out other browsers")}
          </Button>
        )}
      </header>
      {error && !pending && (
        <p role="alert" className="px-5 pt-3 text-sm text-destructive">
          {error}
        </p>
      )}
      {q.isPending ? (
        <p role="status" className="p-5 text-sm text-muted-foreground">
          {phrase("Loading…")}
        </p>
      ) : q.error ? (
        <p role="alert" className="p-5 text-sm text-destructive">
          {q.error.message}
        </p>
      ) : (
        <ul className="divide-y">
          {items.map((s) => {
            const d = describeAgent(s.user_agent);
            const Icon = d.kind === "mobile" ? Smartphone : d.kind === "desktop" ? Monitor : Bot;
            return (
              <li
                key={s.id}
                className={cn(
                  "flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-4",
                  s.current && "bg-success/5",
                )}
              >
                <span
                  className={cn(
                    "flex size-10 shrink-0 items-center justify-center rounded-full",
                    s.current ? "bg-success/15 text-success" : "bg-muted text-muted-foreground",
                  )}
                >
                  <Icon size={18} aria-hidden="true" />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="flex flex-wrap items-center gap-2 text-sm font-medium">
                    {name(s)}
                    {s.current && (
                      <span className="rounded-full bg-success/15 px-2 py-0.5 text-xs font-medium text-success">
                        {phrase("This browser")}
                      </span>
                    )}
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    <span className="font-mono">{s.ip || "—"}</span>
                    {" · "}
                    <time dateTime={s.created_at} title={new Date(s.created_at).toLocaleString()}>
                      {phrase("Signed in")} {when(s.created_at)}
                    </time>
                    {" · "}
                    {phrase("expires in")} {daysLeft(s.expires_at)} {phrase("d")}
                  </p>
                  {!d.browser && s.user_agent && (
                    <p
                      className="mt-0.5 truncate text-xs text-muted-foreground"
                      title={s.user_agent}
                    >
                      {s.user_agent}
                    </p>
                  )}
                </div>
                {s.current ? (
                  <span className="text-xs text-muted-foreground">{phrase("Signed in now")}</span>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                    onClick={() => setPending(s)}
                  >
                    {phrase("Revoke")}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <Dialog open={!!pending} onOpenChange={(open) => !open && setPending(null)}>
        <DialogContent>
          <DialogTitle>
            {pending === "others"
              ? phrase("Sign out every other browser?")
              : phrase("Revoke this session?")}
          </DialogTitle>
          <DialogDescription>
            {pending === "others"
              ? phrase("Every browser except this one is signed out and must sign in again.")
              : pending && (
                  <>
                    {name(pending)} · {pending.ip}
                    {". "}
                    {phrase(
                      "That browser is signed out. This one stays signed in unless you revoke it too.",
                    )}
                  </>
                )}
          </DialogDescription>
          {error && pending && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button variant="destructive" disabled={busy} onClick={revoke}>
              {pending === "others" ? phrase("Sign out others") : phrase("Revoke")}
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
