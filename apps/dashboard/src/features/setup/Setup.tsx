import { useState, type FormEvent, type ReactNode } from "react";
import { Link, Navigate, Outlet, useLocation } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/extra-inputs";
import { BrandIcon } from "@/components/shared/BrandIcon";
import { ThemeToggle } from "@/components/shared/Toolbar";
import { LanguageSwitcher } from "@/features/i18n/LanguageSwitcher";
import { usePhrase } from "@/features/i18n/i18n";
import { useAuth } from "@/features/auth/auth";
import { TwoStep } from "@/features/mail/TwoStep";
import { admin, AdminError } from "@/lib/admin-api";

const setupKey = ["setup-status"] as const;

function useSetupStatus() {
  return useQuery({
    queryKey: setupKey,
    queryFn: () => admin.setupStatus(),
    staleTime: Infinity,
    retry: false,
  });
}

/**
 * While the installation has no user, every page leads to the setup. If the
 * status cannot be read, the app behaves as usual.
 */
export function SetupGate() {
  const status = useSetupStatus();
  const { pathname } = useLocation();
  if (status.data?.needs_setup && pathname !== "/setup") return <Navigate to="/setup" replace />;
  return <Outlet />;
}

const MIN_PASSWORD = 12;

/** A hint only: the server decides what it accepts (at least 12 characters). */
export function passwordStrength(pw: string): "short" | "fair" | "good" | "strong" {
  if (pw.length < MIN_PASSWORD) return "short";
  const kinds = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((r) => r.test(pw)).length;
  if (pw.length >= 20 || (pw.length >= 16 && kinds >= 3)) return "strong";
  if (pw.length >= 16 || kinds >= 3) return "good";
  return "fair";
}

const strengthText = {
  short: "Too short: use at least 12 characters.",
  fair: "Fair. A longer password is stronger.",
  good: "Good password.",
  strong: "Strong password.",
} as const;

const steps = ["Welcome", "Organization", "Your account", "Review", "Done"] as const;

function Frame({ step, children }: { step: number; children: ReactNode }) {
  const phrase = usePhrase();
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex h-14 items-center gap-2 border-b px-4">
        <BrandIcon size={28} />
        <span className="font-[family-name:var(--od-font-wordmark)] font-bold">Njiwa-Mail</span>
        <div className="ml-auto flex items-center gap-1">
          <LanguageSwitcher />
          <ThemeToggle />
        </div>
      </header>
      <main className="mx-auto max-w-lg p-4 sm:p-8">
        <p className="mb-2 text-sm text-muted-foreground">
          {phrase("Step")} {step + 1}/{steps.length}: {phrase(steps[step])}
        </p>
        <div
          className="mb-6 h-2 overflow-hidden rounded-full bg-muted"
          role="progressbar"
          aria-label={phrase("Setup progress")}
          aria-valuemin={1}
          aria-valuemax={steps.length}
          aria-valuenow={step + 1}
        >
          <div
            className="h-full bg-primary transition-all"
            style={{ width: `${((step + 1) / steps.length) * 100}%` }}
          />
        </div>
        <section className="od-card space-y-4 p-6">{children}</section>
      </main>
    </div>
  );
}

function Title({ children, explain }: { children: ReactNode; explain: ReactNode }) {
  return (
    <>
      <h1 className="text-2xl font-bold tracking-tight">{children}</h1>
      <p className="text-sm text-muted-foreground">{explain}</p>
    </>
  );
}

function Nav({ back, next, busy }: { back?: () => void; next: string; busy?: boolean }) {
  const phrase = usePhrase();
  return (
    <div className="flex justify-between gap-3 pt-2">
      {back ? (
        <Button type="button" variant="outline" onClick={back} disabled={busy}>
          {phrase("Back")}
        </Button>
      ) : (
        <span />
      )}
      <Button type="submit" disabled={busy}>
        {next}
      </Button>
    </div>
  );
}

/** First-run setup: organization and first account, once per installation. */
export function Setup() {
  const phrase = usePhrase();
  const status = useSetupStatus();
  const client = useQueryClient();
  const [step, setStep] = useState(0);
  const [form, setForm] = useState({
    organization: "",
    name: "",
    email: "",
    password: "",
    token: "",
    twoStep: false,
  });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const { refresh } = useAuth();
  const set = (k: keyof typeof form, v: string | boolean) => setForm((f) => ({ ...f, [k]: v }));
  const go = (n: number) => {
    setError("");
    setStep(n);
  };

  if (step < 4) {
    if (status.isLoading)
      return (
        <div role="status" className="p-8">
          {phrase("Loading…")}
        </div>
      );
    // Already set up (or it cannot be known): sign in instead.
    if (!status.data?.needs_setup) return <Navigate to="/login" replace />;
  }

  const submit = (fn: () => void) => (e: FormEvent) => {
    e.preventDefault();
    fn();
  };

  const create = async () => {
    setBusy(true);
    setError("");
    try {
      await admin.setup({
        organization: form.organization,
        name: form.name,
        email: form.email,
        password: form.password,
        token: form.token || undefined,
      });
      await refresh();
      setForm((f) => ({ ...f, password: "", token: "" }));
      setStep(4);
      client.setQueryData(setupKey, { needs_setup: false, token_required: false });
    } catch (e) {
      const code = e instanceof AdminError ? e.code : "";
      if (code === "setup_closed") {
        client.setQueryData(setupKey, { needs_setup: false, token_required: false });
        return;
      }
      setError(e instanceof Error ? e.message : phrase("Request failed"));
    } finally {
      setBusy(false);
    }
  };

  const alert = error && (
    <p role="alert" className="text-sm text-destructive">
      {error}
    </p>
  );

  if (step === 0)
    return (
      <Frame step={0}>
        <Title
          explain={phrase(
            "This installation is new. You will name your organization and create the first account. It takes about two minutes, and nothing is sent to anyone.",
          )}
        >
          {phrase("Welcome to Njiwa-Mail")}
        </Title>
        <form onSubmit={submit(() => go(1))}>
          <Nav next={phrase("Start")} />
        </form>
      </Frame>
    );

  if (step === 1)
    return (
      <Frame step={1}>
        <Title
          explain={phrase(
            "The name of your company or team. It appears in the dashboard and in the emails this installation sends to its own users.",
          )}
        >
          {phrase("Name your organization")}
        </Title>
        <form
          className="space-y-3"
          onSubmit={submit(() =>
            form.organization.trim().length < 2
              ? setError(phrase("Use at least 2 characters."))
              : go(2),
          )}
        >
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">{phrase("Organization name")}</span>
            <Input
              value={form.organization}
              maxLength={80}
              autoFocus
              onChange={(e) => set("organization", e.target.value)}
            />
          </label>
          {alert}
          <Nav back={() => go(0)} next={phrase("Next")} />
        </form>
      </Frame>
    );

  if (step === 2) {
    const strength = passwordStrength(form.password);
    return (
      <Frame step={2}>
        <Title
          explain={phrase(
            "This is the first administrator. It can manage everything, including delivery settings and other users.",
          )}
        >
          {phrase("Create your account")}
        </Title>
        <form
          className="space-y-3"
          onSubmit={submit(() => {
            if (form.name.trim().length < 2) return setError(phrase("Use at least 2 characters."));
            if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(form.email.trim()))
              return setError(phrase("Enter an email address."));
            if (strength === "short") return setError(phrase(strengthText.short));
            if (status.data?.token_required && !form.token.trim())
              return setError(phrase("Enter the setup token."));
            go(3);
          })}
        >
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">{phrase("Your name")}</span>
            <Input
              value={form.name}
              maxLength={80}
              autoComplete="name"
              onChange={(e) => set("name", e.target.value)}
            />
          </label>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">{phrase("Email")}</span>
            <Input
              type="email"
              value={form.email}
              autoComplete="username"
              onChange={(e) => set("email", e.target.value)}
            />
          </label>
          <div className="grid gap-1.5 text-sm">
            <label htmlFor="setup-password" className="font-medium">
              {phrase("Password")}
            </label>
            <PasswordInput
              id="setup-password"
              value={form.password}
              autoComplete="new-password"
              onChange={(e) => set("password", e.target.value)}
            />
            <span className="text-xs text-muted-foreground" data-strength={strength}>
              {phrase(strengthText[strength])}
            </span>
          </div>
          {status.data?.token_required && (
            <label className="grid gap-1.5 text-sm">
              <span className="font-medium">{phrase("Setup token")}</span>
              <Input value={form.token} onChange={(e) => set("token", e.target.value)} />
              <span className="text-xs text-muted-foreground">
                {phrase("The value of SETUP_TOKEN in the server's .env file.")}
              </span>
            </label>
          )}
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-1"
              checked={form.twoStep}
              onChange={(e) => set("twoStep", e.target.checked)}
            />
            <span>
              {phrase("Set up two-step sign-in after the account is created (recommended)")}
            </span>
          </label>
          {alert}
          <Nav back={() => go(1)} next={phrase("Next")} />
        </form>
      </Frame>
    );
  }

  if (step === 3)
    return (
      <Frame step={3}>
        <Title explain={phrase("Check the details. Go back to change anything.")}>
          {phrase("Review")}
        </Title>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          {[
            ["Organization name", form.organization],
            ["Your name", form.name],
            ["Email", form.email],
            ["Two-step sign-in", phrase(form.twoStep ? "Set up next" : "Not now")],
          ].map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{phrase(k)}</dt>
              <dd className="break-all">{v}</dd>
            </div>
          ))}
        </dl>
        {alert}
        <form onSubmit={submit(() => void create())}>
          <Nav
            back={() => go(2)}
            busy={busy}
            next={busy ? phrase("Creating…") : phrase("Create account")}
          />
        </form>
      </Frame>
    );

  return <Done twoStep={form.twoStep} />;
}

/** After the account exists: two-step sign-in, the first project, the team. */
function Done({ twoStep }: { twoStep: boolean }) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const { user, refresh } = useAuth();
  const [name, setName] = useState("");
  const [project, setProject] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const createProject = async (e: FormEvent) => {
    e.preventDefault();
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(name))
      return setError(phrase("Use 1–64 letters, digits, dashes or underscores."));
    setBusy(true);
    setError("");
    try {
      await admin.send("/projects", "POST", { name });
      await client.invalidateQueries({ queryKey: ["projects"] });
      setProject(name);
    } catch (err) {
      setError(err instanceof Error ? err.message : phrase("Request failed"));
    } finally {
      setBusy(false);
    }
  };
  const to = (page: string) => `/p/${encodeURIComponent(project)}/${page}`;
  return (
    <Frame step={4}>
      <Title explain={phrase("You are signed in. This setup is now closed for good.")}>
        <CheckCircle2 className="mr-2 inline text-success" aria-hidden />
        {phrase("Your account is ready")}
      </Title>
      {twoStep && <TwoStep enabled={!!user?.mfa_enabled} onChanged={refresh} />}
      {project ? (
        <div className="space-y-3">
          <p className="text-sm">
            {phrase("Project created")}: <strong>{project}</strong>
          </p>
          <div className="flex flex-wrap gap-3">
            <Button asChild>
              <Link to={to("setup")}>{phrase("Continue to Getting started")}</Link>
            </Button>
            <Button asChild variant="outline">
              <Link to={to("users")}>{phrase("Add your team")}</Link>
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            {phrase(
              "Add a user for each colleague, then give them a role in the project under Members.",
            )}
          </p>
        </div>
      ) : (
        <form className="space-y-3" onSubmit={createProject}>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">{phrase("Create your first project")}</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" />
            <span className="text-xs text-muted-foreground">
              {phrase(
                "A project holds one application's domains, senders, keys and messages. You can add your team after this.",
              )}
            </span>
          </label>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex justify-between gap-3">
            <Button asChild variant="outline">
              <Link to="/app">{phrase("Skip for now")}</Link>
            </Button>
            <Button type="submit" disabled={busy || !name}>
              {phrase("Create project")}
            </Button>
          </div>
        </form>
      )}
    </Frame>
  );
}
