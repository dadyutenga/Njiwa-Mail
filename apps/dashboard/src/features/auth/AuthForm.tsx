import { useEffect, useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/extra-inputs";
import { admin, AdminError } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
import { useAuth } from "./auth";

export function AuthForm({ mode }: { mode: string }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [needCode, setNeedCode] = useState(false);
  const [feedback, setFeedback] = useState("");
  const [busy, setBusy] = useState(false);
  const [signedOut] = useState(() => {
    try {
      const v = sessionStorage.getItem("njiwa.signedOut") === "1";
      sessionStorage.removeItem("njiwa.signedOut");
      return v;
    } catch {
      return false;
    }
  });
  const { login, refresh } = useAuth();
  const [providers, setProviders] = useState<{ id: string; name: string }[]>([]);
  const phrase = usePhrase();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const forgot = mode === "forgot-password",
    reset = mode === "reset-password";
  const ssoPending = mode === "login" ? params.get("sso_pending") || "" : "";
  const ssoError = mode === "login" ? params.get("sso_error") || "" : "";
  useEffect(() => {
    if (mode !== "login") return;
    let active = true;
    admin
      .ssoProviders()
      .then((r) => active && setProviders(r.items))
      .catch(() => undefined); // no provider list: the password form still works
    return () => {
      active = false;
    };
  }, [mode]);
  const finishSSO = async (e: FormEvent) => {
    e.preventDefault();
    setFeedback("");
    setBusy(true);
    try {
      await admin.ssoComplete(ssoPending, code);
      await refresh();
      navigate("/app", { replace: true });
    } catch (err) {
      setFeedback(
        err instanceof AdminError && err.code === "sso_failed"
          ? phrase("This sign-in expired. Start again from the sign-in page. Request failed")
          : phrase("That code was not accepted. Request failed"),
      );
    } finally {
      setBusy(false);
    }
  };
  const ssoMessage: Record<string, string> = {
    sso_failed:
      "The provider's sign-in was not accepted. Ask an operator if your account may use it. Request failed",
    too_many_attempts: "Too many sign-in attempts. Try again in a few minutes. Request failed",
    sso_unavailable: "The provider is not available right now. Request failed",
    public_url_missing: "Single sign-on is not set up completely on this server. Request failed",
  };
  const title = phrase(forgot ? "Forgot password" : reset ? "Reset password" : "Welcome back");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setFeedback("");
    if (!forgot && password.length < (reset ? 12 : 8)) {
      setFeedback(
        phrase(
          reset
            ? "Password must be at least 12 characters"
            : "Password must be at least 8 characters",
        ),
      );
      return;
    }
    setBusy(true);
    try {
      if (forgot) {
        await admin.forgot(email);
        setFeedback(phrase("If this account exists, reset instructions have been sent."));
      } else if (reset) {
        const token = params.get("token") || "";
        if (!token) throw new Error(phrase("Reset token is missing."));
        await admin.reset(token, password);
        setFeedback(phrase("Password updated. You can sign in now."));
      } else {
        await login(email, password, needCode ? code : undefined);
        const state = location.state as { from?: string } | null;
        navigate(state?.from || "/app", { replace: true });
      }
    } catch (err) {
      if (err instanceof AdminError && err.code === "mfa_required") {
        setNeedCode(true);
        setFeedback(phrase("Enter the code from your authenticator app, or a recovery code."));
      } else if (err instanceof AdminError && err.code === "invalid_mfa_code") {
        setFeedback(phrase("That code was not accepted. Request failed"));
      } else if (err instanceof AdminError && err.code === "mfa_locked") {
        setFeedback(phrase("Too many wrong codes. Try again in a few minutes. Request failed"));
      } else {
        setFeedback(err instanceof Error ? err.message : phrase("Request failed"));
      }
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="w-full">
      <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        {phrase("Use your Njiwa-Mail operator account.")}
      </p>
      {signedOut && mode === "login" && (
        <p
          role="status"
          className="mt-4 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm"
        >
          {phrase(
            "You were signed out. This happens when the session expires, or after a change to the password, role, operator access or two-step sign-in of this account, or a sign-in with a recovery code.",
          )}
        </p>
      )}
      {ssoError && mode === "login" && (
        <p role="alert" className="mt-4 rounded-lg border p-3 text-sm">
          {phrase(
            ssoMessage[ssoError] ??
              "The provider's sign-in was not accepted. Ask an operator if your account may use it. Request failed",
          )}
        </p>
      )}
      {ssoPending ? (
        <form onSubmit={finishSSO} className="mt-8 space-y-4">
          <label className="block space-y-2 text-sm font-medium">
            {phrase("Authentication code")}
            <Input
              value={code}
              onChange={(e) => setCode(e.target.value)}
              required
              autoFocus
              autoComplete="one-time-code"
              inputMode="text"
            />
            <span className="block text-xs font-normal text-muted-foreground">
              {phrase(
                "Your provider accepted you. Enter the code from your authenticator app, or a recovery code.",
              )}
            </span>
          </label>
          {feedback && (
            <p role="alert" className="rounded-lg border p-3 text-sm">
              {feedback}
            </p>
          )}
          <Button type="submit" disabled={busy} className="w-full">
            {phrase("Sign in")}
          </Button>
        </form>
      ) : (
        <>
          <form onSubmit={submit} className="mt-8 space-y-4">
            {!reset && (
              <label className="block space-y-2 text-sm font-medium">
                {phrase("Email")}
                <Input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  autoComplete="email"
                />
              </label>
            )}
            {!forgot && (
              <label className="block space-y-2 text-sm font-medium">
                {phrase("Password")}
                <PasswordInput
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  autoComplete={reset ? "new-password" : "current-password"}
                />
              </label>
            )}
            {needCode && !forgot && !reset && (
              <label className="block space-y-2 text-sm font-medium">
                {phrase("Authentication code")}
                <Input
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  required
                  autoFocus
                  autoComplete="one-time-code"
                  inputMode="text"
                  aria-describedby="mfa-help"
                />
                <span id="mfa-help" className="block text-xs font-normal text-muted-foreground">
                  {phrase("6 digits from the app, or a recovery code such as ABCDE-FGHIJ.")}
                </span>
              </label>
            )}
            {feedback && (
              <p
                role={
                  feedback.includes("must") ||
                  feedback.includes("failed") ||
                  feedback.includes("missing")
                    ? "alert"
                    : "status"
                }
                className="rounded-lg border p-3 text-sm"
              >
                {feedback}
              </p>
            )}
            <Button type="submit" disabled={busy} className="w-full">
              {phrase(forgot ? "Send Instructions" : reset ? "Reset password" : "Sign in")}
            </Button>
          </form>
          {mode === "login" && providers.length > 0 && (
            <div className="mt-4 space-y-2">
              <p className="text-center text-xs text-muted-foreground">{phrase("or")}</p>
              {providers.map((p) => (
                <Button key={p.id} asChild variant="outline" className="w-full">
                  <a href={`/admin/v1/auth/sso/${encodeURIComponent(p.id)}/start`}>
                    {phrase("Sign in with")} {p.name}
                  </a>
                </Button>
              ))}
            </div>
          )}
        </>
      )}
      <div className="mt-6 flex gap-4 text-sm text-primary">
        {(forgot || reset) && <Link to="/login">{phrase("Sign in")}</Link>}
        {!forgot && !reset && <Link to="/forgot-password">{phrase("Forgot password?")}</Link>}
      </div>
    </div>
  );
}
