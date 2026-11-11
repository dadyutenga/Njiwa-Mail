import { useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/extra-inputs";
import { admin } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
import { useAuth } from "./auth";

export function AuthForm({ mode }: { mode: string }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [feedback, setFeedback] = useState("");
  const [busy, setBusy] = useState(false);
  const { login } = useAuth();
  const phrase = usePhrase();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const forgot = mode === "forgot-password",
    reset = mode === "reset-password";
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
        await login(email, password);
        const state = location.state as { from?: string } | null;
        navigate(state?.from || "/dashboard", { replace: true });
      }
    } catch (err) {
      setFeedback(err instanceof Error ? err.message : phrase("Request failed"));
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
      <div className="mt-6 flex gap-4 text-sm text-primary">
        {(forgot || reset) && <Link to="/login">{phrase("Sign in")}</Link>}
        {!forgot && !reset && <Link to="/forgot-password">{phrase("Forgot password?")}</Link>}
      </div>
    </div>
  );
}
