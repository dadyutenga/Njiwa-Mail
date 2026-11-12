import { useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { admin } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";

type Enrol = { secret: string; otpauth_uri: string };

/**
 * Two-step sign-in (TOTP) for the signed-in user: enrol with an authenticator
 * app, keep the one-time recovery codes, or switch it off again. The secret and
 * the recovery codes are shown once and never stored in the browser.
 */
export function TwoStep({
  enabled,
  onChanged,
}: {
  enabled: boolean;
  onChanged: () => Promise<void> | void;
}) {
  const phrase = usePhrase();
  const [enrol, setEnrol] = useState<Enrol | null>(null);
  const [recovery, setRecovery] = useState<string[] | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const guard = async (fn: () => Promise<void>) => {
    setError("");
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    } finally {
      setBusy(false);
    }
  };
  const start = () =>
    guard(async () => setEnrol(await admin.send<Enrol>("/auth/mfa/enroll", "POST", {})));
  const confirm = (e: FormEvent) => {
    e.preventDefault();
    return guard(async () => {
      const r = await admin.send<{ recovery_codes: string[] }>("/auth/mfa/confirm", "POST", {
        code,
      });
      setRecovery(r.recovery_codes);
      setEnrol(null);
      setCode("");
      await onChanged();
    });
  };
  const disable = (e: FormEvent) => {
    e.preventDefault();
    return guard(async () => {
      await admin.send("/auth/mfa/disable", "POST", { password, code });
      setPassword("");
      setCode("");
      await onChanged();
    });
  };

  return (
    <section className="space-y-3 rounded-xl border bg-card p-5" aria-labelledby="two-step-title">
      <h2 id="two-step-title" className="text-lg font-semibold">
        {phrase("Two-step sign-in")}
      </h2>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      {recovery && (
        <div role="status" className="space-y-2 rounded-lg border p-3 text-sm">
          <p className="font-medium">
            {phrase("Save these recovery codes now. Each works once and they are not shown again.")}
          </p>
          <ul className="grid grid-cols-2 gap-1 font-mono">
            {recovery.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
          <Button type="button" variant="outline" onClick={() => setRecovery(null)}>
            {phrase("I saved them")}
          </Button>
        </div>
      )}
      {!enabled && !enrol && (
        <>
          <p className="text-sm text-muted-foreground">
            {phrase(
              "Add a second step with an authenticator app (Google Authenticator, Aegis, 1Password, …).",
            )}
          </p>
          <Button onClick={start} disabled={busy}>
            {phrase("Set up two-step sign-in")}
          </Button>
        </>
      )}
      {!enabled && enrol && (
        <form onSubmit={confirm} className="space-y-3 text-sm">
          <p>{phrase("In your authenticator app add an account with this key (time based):")}</p>
          <p className="break-all rounded bg-muted p-2 font-mono" data-testid="mfa-secret">
            {enrol.secret}
          </p>
          <p>
            <a className="text-primary underline" href={enrol.otpauth_uri}>
              {phrase("Open in an authenticator app on this device")}
            </a>
          </p>
          <label className="block space-y-1 font-medium">
            {phrase("Code from the app")}
            <input
              className="w-full rounded border bg-background p-2"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              inputMode="numeric"
              autoComplete="one-time-code"
              required
            />
          </label>
          <Button type="submit" disabled={busy}>
            {phrase("Turn on")}
          </Button>
        </form>
      )}
      {enabled && (
        <form onSubmit={disable} className="space-y-3 text-sm">
          <p>{phrase("Two-step sign-in is on for your account.")}</p>
          <label className="block space-y-1 font-medium">
            {phrase("Password")}
            <input
              className="w-full rounded border bg-background p-2"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </label>
          <label className="block space-y-1 font-medium">
            {phrase("Authentication code")}
            <input
              className="w-full rounded border bg-background p-2"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoComplete="one-time-code"
              required
            />
          </label>
          <Button type="submit" variant="outline" disabled={busy}>
            {phrase("Turn off")}
          </Button>
        </form>
      )}
    </section>
  );
}
