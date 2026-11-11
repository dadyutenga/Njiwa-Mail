import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { admin, type Page } from "@/lib/admin-api";
import { useAuth } from "@/features/auth/auth";
import { usePhrase } from "@/features/i18n/i18n";
import { Link } from "react-router-dom";
type Row = Record<string, unknown>;
type Field = {
  key: string;
  label: string;
  type?: "email" | "password" | "select";
  options?: string[];
  required?: boolean;
};
type Config = {
  title: string;
  path: string;
  columns: string[];
  fields?: Field[];
  createLabel?: string;
  owner?: boolean;
};
const project: Field = { key: "project", label: "Project", required: true };
const mode: Field = {
  key: "mode",
  label: "Environment",
  type: "select",
  options: ["sandbox", "live"],
  required: true,
};
const resources: Record<string, Config> = {
  projects: {
    title: "Projects",
    path: "/projects",
    columns: ["name", "enabled", "send_per_minute", "send_per_day", "created_at"],
    fields: [{ key: "name", label: "Project name", required: true }],
  },
  keys: {
    title: "API keys",
    path: "/keys",
    columns: ["prefix", "project", "mode", "label", "scopes", "status", "created_at"],
    fields: [
      project,
      mode,
      { key: "label", label: "Label" },
      { key: "scopes", label: "Scopes (comma-separated)" },
      { key: "ttl_hours", label: "Lifetime in hours" },
    ],
  },
  senders: {
    title: "Senders",
    path: "/senders",
    columns: ["email", "display_name", "project", "mode", "created_at"],
    fields: [
      project,
      mode,
      { key: "email", label: "Sender email", type: "email", required: true },
      { key: "display_name", label: "Display name" },
    ],
  },
  templates: {
    title: "Templates",
    path: "/templates",
    columns: ["slug", "project", "description", "published_version", "draft_version"],
    fields: [
      project,
      { key: "slug", label: "Template slug", required: true },
      { key: "description", label: "Description" },
    ],
  },
  inbound: {
    title: "Inbound mail",
    path: "/inbound",
    columns: ["received_at", "header_from", "envelope_to", "subject", "project", "mode"],
  },
  "inbound-addresses": {
    title: "Inbound addresses",
    path: "/inbound-addresses",
    columns: ["local_part", "project", "mode", "enabled", "route"],
    fields: [
      project,
      mode,
      { key: "local_part", label: "Local part", required: true },
      { key: "route", label: "Route", type: "select", options: ["store", "webhook", "forward"] },
      { key: "webhook_endpoint_id", label: "Webhook endpoint ID" },
      { key: "forward_to", label: "Forward to" },
    ],
  },
  webhooks: {
    title: "Webhooks",
    path: "/webhooks",
    columns: ["url", "project", "mode", "enabled", "events"],
    fields: [
      project,
      mode,
      { key: "url", label: "HTTPS URL", required: true },
      {
        key: "events",
        label:
          "Events (email.submitted, email.failed, email.unknown, email.expired, email.bounced, inbound.received)",
      },
    ],
  },
  "webhook-deliveries": {
    title: "Webhook deliveries",
    path: "/webhook-deliveries",
    columns: ["event_type", "state", "attempts", "last_response_code", "created_at"],
  },
  suppressions: {
    title: "Suppressions",
    path: "/suppressions",
    columns: ["recipient", "reason", "source", "project", "mode", "created_at"],
    fields: [
      project,
      mode,
      { key: "email", label: "Email", type: "email", required: true },
      { key: "reason", label: "Reason", required: true },
    ],
  },
  bounces: {
    title: "Bounces",
    path: "/bounces",
    columns: ["type", "status_code", "recipient", "received_at"],
  },
  alerts: {
    title: "Alerts",
    path: "/alerts",
    columns: ["kind", "summary", "created_at", "acknowledged_at"],
  },
  "alert-channels": {
    title: "Alert channels",
    path: "/alert-channels",
    columns: ["kind", "target", "enabled", "created_at"],
    fields: [
      { key: "kind", label: "Kind", type: "select", options: ["email", "webhook"], required: true },
      { key: "target", label: "Target", required: true },
    ],
  },
  domains: {
    title: "Domains",
    path: "/domains",
    columns: ["name", "project", "purpose", "verified_at", "spf_ok", "dkim_ok", "dmarc_ok"],
    fields: [
      project,
      { key: "name", label: "Domain", required: true },
      { key: "purpose", label: "Purpose", type: "select", options: ["sending", "inbound"] },
    ],
  },
  dns: {
    title: "DNS checks",
    path: "/dns",
    columns: ["domain", "record_type", "pass", "expected", "observed"],
  },
  sandbox: {
    title: "Sandbox captures",
    path: "/sandbox",
    columns: ["id", "template", "project", "created_at", "expires_at"],
  },
  audit: {
    title: "Audit log",
    path: "/audit",
    columns: ["actor", "action", "target_type", "target_id", "at"],
  },
  users: {
    title: "Team users",
    path: "/users",
    columns: ["email", "role", "disabled", "last_login_at", "created_at"],
    owner: true,
    fields: [
      { key: "email", label: "Email", type: "email", required: true },
      {
        key: "password",
        label: "Temporary password (12+ characters)",
        type: "password",
        required: true,
      },
      { key: "role", label: "Role", type: "select", options: ["admin", "owner"], required: true },
    ],
  },
};
function display(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (Array.isArray(value)) return value.map(display).join(", ");
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
export default function Resources({ kind }: { kind: string }) {
  const phrase = usePhrase();
  const cfg = resources[kind];
  const { user } = useAuth();
  const client = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [open, setOpen] = useState(false);
  const [values, setValues] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [secret, setSecret] = useState("");
  const [actionError, setActionError] = useState("");
  const q = useQuery({
    queryKey: ["resource", kind, offset],
    queryFn: () =>
      admin.get<Page<Row> | { items: Row[] }>(
        `${cfg.path}${cfg.path === "/dns" ? "" : `?limit=50&offset=${offset}`}`,
      ),
  });
  if (cfg.owner && user?.role !== "owner")
    return <p role="alert">{phrase("Owner access required.")}</p>;
  const save = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const data: Record<string, unknown> = { ...values };
      for (const field of cfg.fields ?? []) {
        if (field.type === "select" && !data[field.key]) data[field.key] = field.options?.[0];
      }
      for (const [key, value] of Object.entries(data)) {
        if (value === "" && !cfg.fields?.find((field) => field.key === key)?.required)
          delete data[key];
      }
      if (kind === "keys") {
        data.scopes = String(values.scopes || "emails:send,emails:read")
          .split(",")
          .map((s) => s.trim());
        data.ttl_hours = Number(values.ttl_hours || 0);
      }
      if (kind === "webhooks")
        data.events = String(values.events || "email.submitted")
          .split(",")
          .map((s) => s.trim());
      const result = await admin.send<Record<string, string>>(cfg.path, "POST", data);
      if (result?.key || result?.signing_secret || result?.txt)
        setSecret(result.key || result.signing_secret || result.txt);
      setOpen(false);
      setValues({});
      client.invalidateQueries({ queryKey: ["resource", kind] });
    } catch (err) {
      setError(err instanceof Error ? err.message : phrase("Request failed"));
    }
  };
  const rows = q.data?.items || [];
  const hasActions = [
    "keys",
    "senders",
    "webhooks",
    "webhook-deliveries",
    "suppressions",
    "alerts",
    "users",
    "domains",
    "projects",
    "inbound-addresses",
  ].includes(kind);
  const act = async (row: Row, action: string) => {
    setActionError("");
    const id = encodeURIComponent(String(row.id || row.prefix));
    if (!window.confirm(`Confirm ${action}?`)) return;
    try {
      let result: Record<string, string> | undefined;
      if (kind === "keys")
        result = await admin.send(
          `/keys/${id}/${action}`,
          "POST",
          action === "rotate" ? { grace_hours: 24 } : undefined,
        );
      if (kind === "senders") await admin.send(`/senders/${id}`, "DELETE");
      if (kind === "webhooks") {
        if (action === "remove") await admin.send(`/webhooks/${id}`, "DELETE");
        else if (action === "rotate secret")
          result = await admin.send(`/webhooks/${id}/rotate-secret`, "POST");
        else await admin.send(`/webhooks/${id}`, "PATCH", { enabled: !row.enabled });
      }
      if (kind === "inbound-addresses") {
        if (action === "remove") await admin.send(`/inbound-addresses/${id}`, "DELETE");
        else await admin.send(`/inbound-addresses/${id}`, "PATCH", { enabled: !row.enabled });
      }
      if (kind === "projects") {
        const quotas = {
          send_per_minute: Number(row.send_per_minute),
          send_per_day: Number(row.send_per_day),
          recipient_per_minute: Number(row.recipient_per_minute),
          recipient_per_hour: Number(row.recipient_per_hour),
        };
        if (action === "edit quotas") {
          const input = window.prompt(
            "Send/min, send/day, recipient/min, recipient/hour",
            Object.values(quotas).join(","),
          );
          if (!input) return;
          const numbers = input.split(",").map(Number);
          if (numbers.length !== 4 || numbers.some((n) => !Number.isInteger(n) || n < 0)) {
            setActionError(phrase("Enter four nonnegative whole numbers."));
            return;
          }
          Object.assign(quotas, {
            send_per_minute: numbers[0],
            send_per_day: numbers[1],
            recipient_per_minute: numbers[2],
            recipient_per_hour: numbers[3],
          });
        }
        await admin.send(`/projects/${encodeURIComponent(String(row.name))}`, "PATCH", {
          ...quotas,
          ...(action === "edit quotas" ? {} : { enabled: !row.enabled }),
        });
      }
      if (kind === "suppressions") {
        const reason = window.prompt(phrase("Reason for removing this suppression"));
        if (!reason?.trim()) return;
        await admin.send(`/suppressions/${id}`, "DELETE", { reason });
      }
      if (kind === "alerts") await admin.send(`/alerts/${id}/ack`, "POST");
      if (kind === "users")
        if (action === "change role") {
          const role = window.prompt(phrase("New role: owner or admin"), String(row.role));
          if (role !== "owner" && role !== "admin") return;
          await admin.send(`/users/${id}`, "PATCH", { role });
        } else {
          await admin.send(`/users/${id}`, "PATCH", { disabled: action === "disable" });
        }
      if (kind === "domains") await admin.send(`/domains/${id}/verify`, "POST");
      if (kind === "webhook-deliveries")
        await admin.send(`/webhook-deliveries/${id}/replay`, "POST");
      if (result?.key || result?.signing_secret) setSecret(result.key || result.signing_secret);
      client.invalidateQueries({ queryKey: ["resource", kind] });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : phrase("Request failed"));
    }
  };
  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold">{phrase(cfg.title)}</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {phrase("Live data from the Njiwa-Mail admin API.")}
          </p>
        </div>
        {cfg.fields && (
          <button
            className="rounded-md bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground"
            onClick={() => setOpen(!open)}
          >
            {phrase(open ? "Close" : "Add new")}
          </button>
        )}
        {kind === "dns" && (
          <button
            className="rounded-md border px-4 py-2 text-sm"
            onClick={async () => {
              try {
                await admin.send("/dns/check", "POST");
                client.invalidateQueries({ queryKey: ["resource", kind] });
              } catch (err) {
                setActionError(err instanceof Error ? err.message : phrase("DNS check failed"));
              }
            }}
          >
            {phrase("Run DNS check")}
          </button>
        )}
      </div>
      {secret && (
        <div role="status" className="rounded-xl border border-primary p-4">
          <strong>
            {phrase(
              kind === "domains"
                ? "Add this TXT challenge to DNS, then verify the domain."
                : "Copy this secret now. It will not be shown again.",
            )}
          </strong>
          <pre className="mt-2 overflow-x-auto text-sm">{secret}</pre>
          <button onClick={() => setSecret("")} className="mt-2 text-sm text-primary">
            {phrase("Dismiss")}
          </button>
        </div>
      )}
      {actionError && (
        <p role="alert" className="text-destructive">
          {actionError}
        </p>
      )}
      {open && cfg.fields && (
        <form onSubmit={save} className="grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-2">
          {cfg.fields.map((f) => (
            <label key={f.key} className="grid gap-1 text-sm font-medium">
              {phrase(f.label)}
              {f.type === "select" ? (
                <select
                  className="rounded border bg-background p-2"
                  value={values[f.key] || f.options?.[0]}
                  onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                >
                  {f.options?.map((o) => (
                    <option key={o}>{o}</option>
                  ))}
                </select>
              ) : (
                <input
                  className="rounded border bg-background p-2"
                  type={f.type || "text"}
                  required={f.required}
                  value={values[f.key] || ""}
                  onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                />
              )}
            </label>
          ))}
          {error && (
            <p role="alert" className="text-destructive sm:col-span-2">
              {error}
            </p>
          )}
          <button className="rounded bg-primary px-4 py-2 text-primary-foreground sm:col-span-2">
            {phrase("Create")}
          </button>
        </form>
      )}
      {q.isPending ? (
        <p role="status">{phrase("Loading…")}</p>
      ) : q.error ? (
        <p role="alert">{q.error.message}</p>
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border bg-card">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b">
                  {cfg.columns.map((col) => (
                    <th key={col} className="whitespace-nowrap p-3 capitalize">
                      {phrase(col.replaceAll("_", " "))}
                    </th>
                  ))}
                  {hasActions && <th className="p-3">{phrase("Actions")}</th>}
                </tr>
              </thead>
              <tbody>
                {rows.map((row, i) => (
                  <tr key={String(row.id || row.prefix || i)} className="border-b">
                    {cfg.columns.map((col) => (
                      <td className="max-w-64 truncate p-3" title={phrase(display(row[col]))} key={col}>
                        {kind === "projects" && col === "name" ? (
                          <Link
                            className="text-primary underline"
                            to={`/projects/${encodeURIComponent(String(row.name))}`}
                          >
                            {phrase(display(row[col]))}
                          </Link>
                        ) : kind === "templates" && col === "slug" ? (
                          <Link
                            className="text-primary underline"
                            to={`/templates/${encodeURIComponent(String(row.id))}`}
                          >
                            {phrase(display(row[col]))}
                          </Link>
                        ) : (kind === "inbound" && col === "subject") ||
                          (kind === "sandbox" && col === "id") ? (
                          <Link
                            className="text-primary underline"
                            to={`/${kind}/${encodeURIComponent(String(row.id))}`}
                          >
                            {phrase(display(row[col]))}
                          </Link>
                        ) : (
                          phrase(display(row[col]))
                        )}
                      </td>
                    ))}
                    {hasActions && (
                      <td className="whitespace-nowrap p-3 text-primary">
                        {(kind === "projects"
                          ? [row.enabled ? "disable" : "enable", "edit quotas"]
                          : kind === "inbound-addresses"
                            ? [row.enabled ? "disable" : "enable", "remove"]
                            : kind === "webhooks"
                              ? [row.enabled ? "disable" : "enable", "rotate secret", "remove"]
                              : kind === "keys"
                                ? ["rotate", "revoke"]
                                : kind === "users"
                                  ? [row.disabled ? "enable" : "disable", "change role"]
                                  : kind === "webhook-deliveries"
                                    ? ["replay"]
                                    : kind === "domains"
                                      ? ["verify"]
                                      : kind === "alerts"
                                        ? ["acknowledge"]
                                        : ["remove"]
                        ).map((action) => (
                          <button
                            key={action}
                            className="mr-3 underline"
                            onClick={() => act(row, action)}
                          >
                            {phrase(action)}
                          </button>
                        ))}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
            {!rows.length && (
              <p className="p-5 text-muted-foreground">{phrase("No records yet.")}</p>
            )}
          </div>
          {"has_more" in (q.data || {}) && (
            <div className="flex gap-4 text-sm">
              <button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 50))}>
                {phrase("Previous")}
              </button>
              <button
                disabled={!(q.data as Page<Row>).has_more}
                onClick={() => setOffset(offset + 50)}
              >
                {phrase("Next")}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
