import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { useWorkspace, useWsKey, useSendingEnvironment, type Env } from "../context";
import {
  ConfirmButton,
  CopyButton,
  DataState,
  Empty,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  Pager,
  run,
  s,
  SecretOnce,
  Section,
  selectClass,
  StateBadge,
  Table,
  Time,
  useOffset,
  useParam,
  type Page,
  type Row,
} from "../ui";

const SCOPES: [string, string][] = [
  ["emails:send", "Send with templates"],
  ["emails:send_raw", "Send written messages (subject and body)"],
  ["emails:read", "Read message status"],
  ["inbound:read", "Read inbound mail"],
  ["suppressions:manage", "Manage suppressions"],
];
const EVENTS = [
  "email.submitted",
  "email.failed",
  "email.unknown",
  "email.expired",
  "email.bounced",
  "inbound.received",
];

import { SendingMode, useSendingModeParam } from "../SendingMode";

// ---------------------------------------------------------------- API keys

export function ApiKeys() {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const { api, key } = useSendingEnvironment(env);
  const client = useQueryClient();
  const [secret, setSecret] = useState("");
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: key("keys"), queryFn: () => api.get<Page>("/keys?limit=100") });
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("keys") });
    client.invalidateQueries({ queryKey: key("checklist") });
  };
  return (
    <>
      <PageHeader
        title={phrase("API keys")}
        description={`${phrase("Keys for applications to send from this project's")} ${env === "live" ? phrase("Live") : phrase("Sandbox")} ${phrase("environment. A Sandbox key can never send real mail.")}`}
        action={
          <Button onClick={() => setOpen(true)}>
            <Plus size={16} /> {phrase("Create API key")}
          </Button>
        }
      />
      <div className="mb-4">
        <SendingMode
          value={env}
          onChange={(mode) => {
            setEnv(mode);
            setSecret("");
          }}
        />
      </div>
      {secret && (
        <SecretOnce
          title={phrase("Your new API key")}
          value={secret}
          onDone={() => setSecret("")}
        />
      )}
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No API keys")}
              description={phrase("Create one for each application that sends mail.")}
              action={<Button onClick={() => setOpen(true)}>{phrase("Create API key")}</Button>}
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.prefix)}
            columns={[
              {
                key: "prefix",
                label: phrase("Key"),
                render: (r) => (
                  <span className="flex items-center gap-2">
                    <code>{s(r.prefix)}…</code> <CopyButton text={s(r.prefix)} label="key prefix" />
                  </span>
                ),
              },
              { key: "label", label: phrase("Label") },
              {
                key: "scopes",
                label: phrase("Permissions"),
                render: (r) => ((r.scopes as string[]) ?? []).join(", "),
              },
              {
                key: "status",
                label: phrase("State"),
                render: (r) => <StateBadge state={s(r.status)} />,
              },
              {
                key: "last_used_at",
                label: phrase("Last used"),
                render: (r) => <Time value={r.last_used_at} relative />,
              },
              {
                key: "messages",
                label: phrase("Messages"),
                render: (r) => (
                  <Link
                    className="text-primary"
                    to={to(`messages?api_key=${encodeURIComponent(s(r.prefix))}`)}
                  >
                    {s(r.messages)}
                  </Link>
                ),
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) =>
                  r.status === "active" ? (
                    <span className="flex gap-1">
                      <ConfirmButton
                        destructive={false}
                        title={phrase("Rotate this key?")}
                        description={phrase(
                          "A new key is created and shown once. The old key keeps working for 24 hours so you can switch over, then stops.",
                        )}
                        confirm="Rotate"
                        onConfirm={async () => {
                          const res = await run(
                            () => api.send<{ key: string }>(`/keys/${s(r.prefix)}/rotate`, "POST"),
                            phrase("Key rotated."),
                          );
                          if (res) setSecret(res.key);
                          refresh();
                        }}
                      >
                        {phrase("Rotate")}
                      </ConfirmButton>
                      <ConfirmButton
                        title={`${phrase("Revoke")} ${s(r.prefix)}?`}
                        description={phrase(
                          "Applications using this key stop sending at once. This cannot be undone.",
                        )}
                        confirm="Revoke key"
                        onConfirm={async () => {
                          await run(
                            () => api.send(`/keys/${s(r.prefix)}/revoke`, "POST"),
                            phrase("Key revoked."),
                          );
                          refresh();
                        }}
                      >
                        {phrase("Revoke")}
                      </ConfirmButton>
                    </span>
                  ) : null,
              },
            ]}
          />
        )}
      </DataState>
      <KeyDialog mode={env} open={open} onOpenChange={setOpen} onCreated={(k) => setSecret(k)} />
    </>
  );
}

function KeyDialog({
  mode,
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (key: string) => void;
  mode: Env;
}) {
  const phrase = usePhrase();
  const { api, key } = useSendingEnvironment(mode);
  const client = useQueryClient();
  const [label, setLabel] = useState("");
  const [scopes, setScopes] = useState<string[]>(["emails:send", "emails:read"]);
  const [ttl, setTtl] = useState("0");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("Create API key")}</DialogTitle>
        <DialogDescription>
          {phrase("The secret is shown once, right after you create it.")}
        </DialogDescription>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!scopes.length) return setError(phrase("Choose at least one permission."));
            setBusy(true);
            setError("");
            try {
              const r = await api.send<{ key: string }>("/keys", "POST", {
                label,
                scopes,
                ttl_hours: Number(ttl),
              });
              client.invalidateQueries({ queryKey: key("keys") });
              client.invalidateQueries({ queryKey: key("checklist") });
              onCreated(r.key);
              onOpenChange(false);
              setLabel("");
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field
            label={phrase("Label")}
            hint={phrase("Which application uses it, for example “billing service”.")}
            error={error}
          >
            <input
              autoFocus
              className={inputClass}
              value={label}
              maxLength={100}
              onChange={(e) => setLabel(e.target.value)}
            />
          </Field>
          <fieldset className="space-y-1">
            <legend className="mb-1 text-sm font-medium">{phrase("Permissions")}</legend>
            {SCOPES.map(([v, l]) => (
              <label key={v} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={scopes.includes(v)}
                  onChange={(e) =>
                    setScopes(e.target.checked ? [...scopes, v] : scopes.filter((x) => x !== v))
                  }
                />
                {phrase(l)} <code className="text-xs text-muted-foreground">{v}</code>
              </label>
            ))}
          </fieldset>
          <Field label={phrase("Expires")}>
            <select className={selectClass} value={ttl} onChange={(e) => setTtl(e.target.value)}>
              <option value="0">{phrase("Never")}</option>
              <option value="720">{phrase("In 30 days")}</option>
              <option value="2160">{phrase("In 90 days")}</option>
              <option value="8760">{phrase("In one year")}</option>
            </select>
          </Field>
          <Button type="submit" disabled={busy}>
            {busy ? phrase("Creating…") : phrase("Create key")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- webhooks

export function Webhooks() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [secret, setSecret] = useState("");
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<string[]>([
    "email.submitted",
    "email.failed",
    "email.bounced",
  ]);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: key("webhooks"),
    queryFn: () => api.get<Page>("/webhooks?limit=100"),
  });
  const urlOk = /^https:\/\/[^\s/]+/.test(url);
  return (
    <>
      <PageHeader
        title={phrase("Webhooks")}
        description={phrase(
          "Njiwa-Mail calls these HTTPS endpoints when something happens to your mail. Each call is signed.",
        )}
      />
      {secret && (
        <SecretOnce title={phrase("Signing secret")} value={secret} onDone={() => setSecret("")} />
      )}
      <form
        className="mb-4 grid gap-3 rounded-xl border bg-card p-4"
        onSubmit={async (e) => {
          e.preventDefault();
          if (!urlOk) return setError(phrase("The URL must start with https://"));
          if (!events.length) return setError(phrase("Choose at least one event."));
          setError("");
          const r = await run(
            () =>
              api.send<{ id: string; signing_secret: string }>("/webhooks", "POST", {
                url,
                events,
              }),
            phrase("Endpoint added."),
          );
          if (r) {
            setSecret(r.signing_secret);
            setUrl("");
            client.invalidateQueries({ queryKey: key("webhooks") });
          }
        }}
      >
        <Field label={phrase("Endpoint URL")} error={error}>
          <input
            className={inputClass}
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://app.example.com/hooks/njiwa"
          />
        </Field>
        <fieldset className="flex flex-wrap gap-x-4 gap-y-1">
          <legend className="mb-1 text-sm font-medium">{phrase("Events")}</legend>
          {EVENTS.map((ev) => (
            <label key={ev} className="flex items-center gap-1.5 text-sm">
              <input
                type="checkbox"
                checked={events.includes(ev)}
                onChange={(e) =>
                  setEvents(e.target.checked ? [...events, ev] : events.filter((x) => x !== ev))
                }
              />{" "}
              {ev}
            </label>
          ))}
        </fieldset>
        <Button type="submit" className="justify-self-start">
          {phrase("Add endpoint")}
        </Button>
      </form>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty title={phrase("No webhook endpoints")} />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            onRow={(r) => navigate(to(`webhooks/${s(r.id)}`))}
            columns={[
              {
                key: "url",
                label: "URL",
                render: (r) => (
                  <Link className="hover:underline" to={to(`webhooks/${s(r.id)}`)}>
                    {s(r.url)}
                  </Link>
                ),
              },
              {
                key: "events",
                label: phrase("Events"),
                render: (r) => ((r.events as string[]) ?? []).join(", "),
              },
              {
                key: "enabled",
                label: phrase("State"),
                render: (r) => (
                  <StateBadge
                    state={r.enabled ? "active" : "cancelled"}
                    label={
                      r.enabled ? phrase("enabled") : s(r.disabled_reason) || phrase("disabled")
                    }
                  />
                ),
              },
              {
                key: "failed",
                label: phrase("Failed deliveries"),
                render: (r) =>
                  Number(r.failed_deliveries) ? (
                    <Link className="text-destructive" to={to(`webhooks/${s(r.id)}?state=dead`)}>
                      {s(r.failed_deliveries)}
                    </Link>
                  ) : (
                    "0"
                  ),
              },
            ]}
          />
        )}
      </DataState>
    </>
  );
}

export function WebhookDetail() {
  const phrase = usePhrase();
  const { id = "" } = useParams();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [state, setState] = useParam("state");
  const [secret, setSecret] = useState("");
  const offset = useOffset();
  const endpoints = useQuery({
    queryKey: key("webhooks"),
    queryFn: () => api.get<Page>("/webhooks?limit=100"),
  });
  const ep = endpoints.data?.items.find((x) => s(x.id) === id);
  const deliveries = useQuery({
    queryKey: key("deliveries", id, state, offset),
    queryFn: () =>
      api.get<Page>(
        `/webhook-deliveries?endpoint_id=${id}&limit=50&offset=${offset}${state ? `&state=${state}` : ""}`,
      ),
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("webhooks") });
    client.invalidateQueries({ queryKey: key("deliveries") });
  };
  return (
    <>
      <PageHeader
        back={{ to: to("webhooks"), label: phrase("Webhooks") }}
        title={ep ? s(ep.url) : phrase("Webhook endpoint")}
        description={ep ? ((ep.events as string[]) ?? []).join(", ") : undefined}
        action={
          ep && (
            <>
              <Button
                variant="outline"
                onClick={async () => {
                  await run(
                    () => api.send(`/webhooks/${id}`, "PATCH", { enabled: !ep.enabled }),
                    ep.enabled ? phrase("Disabled.") : phrase("Enabled."),
                  );
                  refresh();
                }}
              >
                {ep.enabled ? phrase("Disable") : phrase("Enable")}
              </Button>
              <ConfirmButton
                size="default"
                destructive={false}
                title={phrase("Rotate the signing secret?")}
                description={phrase(
                  "The old secret stops working at once. Update your receiver with the new one.",
                )}
                confirm="Rotate secret"
                onConfirm={async () => {
                  const r = await run(
                    () =>
                      api.send<{ signing_secret: string }>(`/webhooks/${id}/rotate-secret`, "POST"),
                    phrase("Secret rotated."),
                  );
                  if (r) setSecret(r.signing_secret);
                }}
              >
                {phrase("Rotate secret")}
              </ConfirmButton>
              <ConfirmButton
                size="default"
                title={phrase("Delete this endpoint?")}
                description={phrase(
                  "No more events are sent to it. Pending deliveries are dropped.",
                )}
                confirm="Delete endpoint"
                onConfirm={async () => {
                  if (
                    await run(
                      () => api.send(`/webhooks/${id}`, "DELETE"),
                      phrase("Endpoint deleted."),
                    )
                  ) {
                    refresh();
                    navigate(to("webhooks"));
                  }
                }}
              >
                {phrase("Delete")}
              </ConfirmButton>
            </>
          )
        }
      />
      {secret && (
        <SecretOnce
          title={phrase("New signing secret")}
          value={secret}
          onDone={() => setSecret("")}
        />
      )}
      <div className="mb-3 w-48">
        <Field label={phrase("Deliveries")}>
          <select className={selectClass} value={state} onChange={(e) => setState(e.target.value)}>
            <option value="">{phrase("All")}</option>
            <option value="dead">{phrase("Failed")}</option>
            <option value="pending">{phrase("Waiting to retry")}</option>
            <option value="succeeded">{phrase("Succeeded")}</option>
          </select>
        </Field>
      </div>
      <DataState
        q={deliveries}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty title={phrase("No deliveries")} />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                { key: "event_type", label: phrase("Event") },
                {
                  key: "state",
                  label: phrase("State"),
                  render: (r) => (
                    <StateBadge
                      state={
                        s(r.state) === "succeeded"
                          ? "submitted"
                          : s(r.state) === "dead"
                            ? "failed"
                            : "pending"
                      }
                      label={s(r.state)}
                    />
                  ),
                },
                { key: "attempts", label: phrase("Attempts") },
                {
                  key: "code",
                  label: phrase("Last response"),
                  render: (r) => s(r.last_response_code) || s(r.last_error) || "—",
                },
                {
                  key: "message",
                  label: phrase("Message"),
                  render: (r) =>
                    r.message_id ? (
                      <Link className="text-primary" to={to(`messages/${s(r.message_id)}`)}>
                        {phrase("Open")}
                      </Link>
                    ) : (
                      "—"
                    ),
                },
                {
                  key: "created_at",
                  label: phrase("Created"),
                  render: (r) => <Time value={r.created_at} />,
                },
                {
                  key: "x",
                  label: <span className="sr-only">{phrase("Actions")}</span>,
                  render: (r) =>
                    r.state === "dead" ? (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={async () => {
                          await run(
                            () => api.send(`/webhook-deliveries/${s(r.id)}/replay`, "POST"),
                            phrase("Delivery queued again."),
                          );
                          refresh();
                        }}
                      >
                        {phrase("Replay")}
                      </Button>
                    ) : null,
                },
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
    </>
  );
}

// ---------------------------------------------------------------- API docs

export function ApiDocs() {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const { info } = useSendingEnvironment(env);
  const ops = useQuery({
    queryKey: ["api-docs"],
    queryFn: () =>
      admin.get<{ operations: { method: string; path: string; purpose: string }[] }>("/docs"),
  });
  const host = info?.api_host || window.location.origin;
  const sender = info?.default_sender || "hello@your-verified-domain";
  const curl = `curl -X POST ${host}/v1/emails \\
  -H "Authorization: Bearer $NJIWA_${env.toUpperCase()}_KEY" \\
  -H "Idempotency-Key: $(uuidgen)" \\
  -H "Content-Type: application/json" \\
  -d '{
    "from": "${sender}",
    "to": "someone@example.org",
    "subject": "Hello from Njiwa-Mail",
    "text": "It works."
  }'`;
  return (
    <>
      <PageHeader
        title={phrase("API docs")}
        description={phrase(
          "Applications send through the /v1 API with an API key of this project and environment.",
        )}
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} />
      </div>
      <Section title={phrase("Quick start")}>
        <ol className="mb-3 list-decimal space-y-1 pl-5 text-sm">
          <li>
            <Link className="text-primary" to={to("keys")}>
              {phrase("Create an API key")}
            </Link>{" "}
            {phrase("with the “Send written messages” permission.")}
          </li>
          <li>{phrase("Run this request. The sender is this project's default sender.")}</li>
        </ol>
        <div className="relative">
          <pre className="overflow-x-auto rounded-lg bg-muted/50 p-3 text-xs">{curl}</pre>
          <div className="absolute right-2 top-2">
            <CopyButton text={curl} label="request" />
          </div>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          {phrase(
            "The response is 202 with an id and status “queued”. Repeat the same Idempotency-Key to retry safely; it never sends twice.",
          )}
          {!info?.default_sender && (
            <>
              {" "}
              <Link className="text-primary" to={to("senders")}>
                {phrase("Add a sender first.")}
              </Link>
            </>
          )}
        </p>
        <p className="mt-2 flex items-center gap-2 text-xs">
          {phrase("API host")}: <code>{host}</code> <CopyButton text={host} label="API host" />
        </p>
      </Section>
      <Section className="mt-4" title={phrase("Operations")}>
        <DataState q={ops}>
          {(d) => (
            <Table
              rows={d.operations}
              rowKey={(r) => s(r.method) + s(r.path)}
              columns={[
                {
                  key: "method",
                  label: phrase("Method"),
                  render: (r) => <code>{s(r.method)}</code>,
                },
                { key: "path", label: phrase("Path"), render: (r) => <code>{s(r.path)}</code> },
                { key: "purpose", label: phrase("Purpose") },
              ]}
            />
          )}
        </DataState>
        <p className="mt-3 text-xs text-muted-foreground">
          {phrase(
            "Message states: queued, scheduled, submitting, submitted, retry_wait, failed, unknown, expired, captured (sandbox), cancelled. There is no “delivered” state.",
          )}
        </p>
      </Section>
    </>
  );
}
