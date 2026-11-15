import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { admin, type Page } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";

const field = "w-full rounded border bg-background p-2";

type AuditRow = { id: number; at: string; action: string; target_type: string };

/**
 * The signed-in user's own account: change the password (other devices are signed out),
 * sign out every other device, ask for the account to be deleted after a grace period, and
 * read the account's own activity.
 */
export function AccountSecurity({
  mfa,
  deletionDueAt,
  onChanged,
}: {
  mfa: boolean;
  deletionDueAt?: string | null;
  onChanged: () => Promise<void> | void;
}) {
  const phrase = usePhrase();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");
  const [delPass, setDelPass] = useState("");
  const [delCode, setDelCode] = useState("");
  const [delOpen, setDelOpen] = useState(false);
  const [showAudit, setShowAudit] = useState(false);
  const audit = useQuery({
    queryKey: ["own-audit"],
    queryFn: () => admin.get<Page<AuditRow>>("/auth/audit?limit=50"),
    enabled: showAudit,
  });
  const guard = async (fn: () => Promise<void>) => {
    setError("");
    setMsg("");
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    }
  };
  const change = (e: FormEvent) => {
    e.preventDefault();
    if (next !== again) {
      setError(phrase("The two new passwords are not the same."));
      return Promise.resolve();
    }
    return guard(async () => {
      await admin.send("/auth/password", "POST", { current_password: current, new_password: next });
      setCurrent("");
      setNext("");
      setAgain("");
      setMsg(phrase("Password changed. Your other devices were signed out."));
    });
  };
  const others = () =>
    guard(async () => {
      if (!window.confirm(phrase("Sign out every other device?"))) return;
      const r = await admin.send<{ revoked: number }>("/auth/sessions/revoke-others", "POST", {});
      setMsg(`${phrase("Devices signed out")}: ${r.revoked}`);
    });
  const requestDelete = (e: FormEvent) => {
    e.preventDefault();
    return guard(async () => {
      await admin.send("/auth/delete-account", "POST", { password: delPass, code: delCode });
      setDelPass("");
      setDelCode("");
      setDelOpen(false);
      await onChanged();
    });
  };
  const cancelDelete = () =>
    guard(async () => {
      await admin.send("/auth/cancel-deletion", "POST", {});
      await onChanged();
    });
  return (
    <>
      <section className="rounded-xl border bg-card p-5">
        <h2 className="text-lg font-semibold">{phrase("Password")}</h2>
        <form onSubmit={change} className="mt-3 max-w-md space-y-3 text-sm">
          <label className="block space-y-1 font-medium">
            {phrase("Current password")}
            <input
              className={field}
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              required
            />
          </label>
          <label className="block space-y-1 font-medium">
            {phrase("New password")}
            <input
              className={field}
              type="password"
              autoComplete="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              required
            />
          </label>
          <label className="block space-y-1 font-medium">
            {phrase("Repeat the new password")}
            <input
              className={field}
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
              required
            />
          </label>
          <div className="flex flex-wrap gap-2">
            <Button type="submit">{phrase("Change password")}</Button>
            <Button type="button" variant="outline" onClick={others}>
              {phrase("Sign out other devices")}
            </Button>
          </div>
        </form>
        {msg && (
          <p role="status" className="mt-3 text-sm">
            {msg}
          </p>
        )}
        {error && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {error}
          </p>
        )}
      </section>
      <section className="rounded-xl border bg-card p-5">
        <h2 className="text-lg font-semibold">{phrase("My activity")}</h2>
        {!showAudit ? (
          <Button className="mt-3" variant="outline" onClick={() => setShowAudit(true)}>
            {phrase("Show my activity")}
          </Button>
        ) : audit.isPending ? (
          <p role="status">{phrase("Loading…")}</p>
        ) : audit.error ? (
          <p role="alert">{audit.error.message}</p>
        ) : (
          <ul className="mt-3 space-y-1 text-sm">
            {audit.data.items.map((a) => (
              <li key={a.id} className="flex justify-between gap-3 border-t py-1">
                <code>{a.action}</code>
                <span className="text-muted-foreground">{new Date(a.at).toLocaleString()}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="rounded-xl border border-destructive/40 bg-card p-5">
        <h2 className="text-lg font-semibold">{phrase("Delete my account")}</h2>
        {deletionDueAt ? (
          <div className="mt-3 space-y-3 text-sm">
            <p role="status">
              {phrase("Your account will be deleted on")} {new Date(deletionDueAt).toLocaleString()}
              . {phrase("Until then you can keep working or cancel.")}
            </p>
            <Button variant="outline" onClick={cancelDelete}>
              {phrase("Keep my account")}
            </Button>
          </div>
        ) : !delOpen ? (
          <div className="mt-3 space-y-3 text-sm">
            <p>
              {phrase(
                "Your account is removed 14 days after you ask. Sign-in keeps working until then, and you can cancel. The last user, or the last installation operator, cannot leave.",
              )}
            </p>
            <Button variant="outline" onClick={() => setDelOpen(true)}>
              {phrase("Delete my account")}
            </Button>
          </div>
        ) : (
          <form onSubmit={requestDelete} className="mt-3 max-w-md space-y-3 text-sm">
            <label className="block space-y-1 font-medium">
              {phrase("Password")}
              <input
                className={field}
                type="password"
                autoComplete="current-password"
                value={delPass}
                onChange={(e) => setDelPass(e.target.value)}
                required
              />
            </label>
            {mfa && (
              <label className="block space-y-1 font-medium">
                {phrase("Authentication code")}
                <input
                  className={field}
                  value={delCode}
                  onChange={(e) => setDelCode(e.target.value)}
                  autoComplete="one-time-code"
                  required
                />
              </label>
            )}
            <div className="flex gap-2">
              <Button type="submit" variant="destructive">
                {phrase("Schedule deletion")}
              </Button>
              <Button type="button" variant="outline" onClick={() => setDelOpen(false)}>
                {phrase("Cancel")}
              </Button>
            </div>
          </form>
        )}
      </section>
    </>
  );
}
