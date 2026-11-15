import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUpRight, Server, ShieldCheck, Users } from "lucide-react";
import { Link } from "react-router-dom";
import { admin } from "@/lib/admin-api";
import { useWorkspace } from "@/features/workspace/context";
import { usePhrase } from "@/features/i18n/i18n";
import { inputClass } from "@/features/workspace/ui";

const access = [
  {
    path: "users",
    title: "Users",
    detail: "People who can sign in, and who may manage the installation.",
    icon: Users,
  },
  {
    path: "mail-server",
    title: "Delivery infrastructure",
    detail: "How this installation hands mail to a server.",
    icon: Server,
  },
  {
    path: "privacy",
    title: "Recipient privacy",
    detail: "Export or delete the personal data held for a recipient.",
    icon: ShieldCheck,
  },
] as const;

const groups: { title: string; keys: { key: string; label: string; hint: string }[] }[] = [
  {
    title: "Sign-in",
    keys: [
      {
        key: "login_attempts_per_account",
        label: "Sign-in attempts per account",
        hint: "Sign-in attempts allowed for one account in 15 minutes.",
      },
      {
        key: "login_attempts_per_ip",
        label: "Sign-in attempts per address",
        hint: "Sign-in attempts allowed from one network address in 15 minutes.",
      },
    ],
  },
  {
    title: "Bounce alerts",
    keys: [
      {
        key: "bounce_alert_min_volume",
        label: "Minimum volume",
        hint: "Submissions in the last hour before a bounce rate raises an alert.",
      },
      {
        key: "bounce_alert_hard_percent",
        label: "Hard bounce percent",
        hint: "Hard bounces as a percent of submissions in the last hour.",
      },
      {
        key: "bounce_alert_policy_percent",
        label: "Policy bounce percent",
        hint: "Policy bounces as a percent of submissions in the last hour.",
      },
    ],
  },
  {
    title: "New projects",
    keys: [
      {
        key: "default_send_per_minute",
        label: "Sends per minute",
        hint: "Send rate applied when a project is created.",
      },
      {
        key: "default_send_per_day",
        label: "Sends per day",
        hint: "Daily send limit applied when a project is created.",
      },
    ],
  },
  {
    title: "Retention",
    keys: [
      {
        key: "inbound_retention_days",
        label: "Inbound mail (days)",
        hint: "How long received mail is kept.",
      },
      {
        key: "delivery_events_retention_days",
        label: "Delivery evidence (days)",
        hint: "How long delivery events are kept.",
      },
      {
        key: "installer_receipts_retention_days",
        label: "Installer receipts (days)",
        hint: "How long installer receipts are kept.",
      },
    ],
  },
  {
    title: "Webhooks",
    keys: [
      {
        key: "webhook_auto_disable_after",
        label: "Failures before disable",
        hint: "Consecutive failed deliveries before a webhook is turned off.",
      },
    ],
  },
];

const known = new Set(groups.flatMap((g) => g.keys.map((k) => k.key)));

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
  const extra = Object.keys(values)
    .filter((key) => key !== "require_mfa" && !known.has(key))
    .sort();
  const listing = extra.length
    ? [
        ...groups,
        {
          title: "Other",
          keys: extra.map((key) => ({ key, label: key.replaceAll("_", " "), hint: "" })),
        },
      ]
    : groups;

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">{phrase("Settings")}</h1>
      <p className="text-muted-foreground">{phrase("Operator limits and retention settings.")}</p>
      <section className="space-y-4 rounded-xl border bg-card p-5">
        <div>
          <h2 className="text-lg font-semibold">{phrase("Shared access")}</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {phrase("Every enabled user is an admin and can access all pages and settings.")}
          </p>
        </div>
        <nav aria-label={phrase("Administration settings")} className="grid gap-3 md:grid-cols-3">
          {access.map((item) => (
            <Link
              key={item.path}
              to={to(item.path)}
              className="group flex h-full flex-col rounded-lg border bg-background p-4 transition hover:border-primary hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="flex items-start justify-between gap-3">
                <span className="flex size-9 items-center justify-center rounded-md bg-primary/10 text-primary">
                  <item.icon size={18} aria-hidden />
                </span>
                <ArrowUpRight
                  size={16}
                  aria-hidden
                  className="text-muted-foreground transition group-hover:text-primary"
                />
              </span>
              <span className="mt-3 font-medium">{phrase(item.title)}</span>
              <span className="mt-1 text-xs leading-5 text-muted-foreground">
                {phrase(item.detail)}
              </span>
            </Link>
          ))}
        </nav>
      </section>
      {q.isPending ? (
        <p role="status">{phrase("Loading settings…")}</p>
      ) : q.error ? (
        <p role="alert">{q.error.message}</p>
      ) : (
        <form
          className="overflow-hidden rounded-xl border bg-card"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <div className="flex items-center justify-between gap-4 border-b px-5 py-4">
            <div>
              <h2 className="font-semibold">{phrase("Limits")}</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                {phrase("Numbers apply to the whole installation.")}
              </p>
            </div>
          </div>
          <label className="flex items-start gap-3 border-b px-5 py-4">
            <input
              type="checkbox"
              className="mt-1"
              checked={values.require_mfa === 1}
              onChange={(e) => setValues({ ...values, require_mfa: e.target.checked ? 1 : 0 })}
            />
            <span>
              <span className="block text-sm font-medium">
                {phrase("Require two-step sign-in for every admin")}
              </span>
              <span className="mt-1 block text-xs text-muted-foreground">
                {phrase("An admin without a second factor is asked to enrol at the next sign-in.")}
              </span>
            </span>
          </label>
          {listing.map((group) => (
            <section key={group.title}>
              <h3 className="border-b bg-muted/40 px-5 py-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                {phrase(group.title)}
              </h3>
              <ul>
                {group.keys.map((field) => (
                  <li
                    key={field.key}
                    className="grid gap-3 border-b px-5 py-4 last:border-b-0 sm:grid-cols-[1fr_8rem] sm:items-center"
                  >
                    <label htmlFor={field.key}>
                      <span className="block text-sm font-medium">{phrase(field.label)}</span>
                      {field.hint && (
                        <span className="mt-1 block text-xs text-muted-foreground">
                          {phrase(field.hint)}
                        </span>
                      )}
                    </label>
                    <input
                      id={field.key}
                      type="number"
                      min="0"
                      value={values[field.key] ?? ""}
                      onChange={(e) =>
                        setValues({ ...values, [field.key]: Number(e.target.value) })
                      }
                      className={inputClass}
                    />
                  </li>
                ))}
              </ul>
            </section>
          ))}
          <div className="flex flex-wrap items-center gap-3 border-t bg-muted/20 px-5 py-4">
            <button type="submit" className="rounded bg-primary px-4 py-2 text-primary-foreground">
              {phrase("Save settings")}
            </button>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            {saved && (
              <p role="status" className="text-sm">
                {phrase("Settings saved.")}
              </p>
            )}
          </div>
        </form>
      )}
    </div>
  );
}
