import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Star, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import {
  ConfirmButton,
  DataState,
  Empty,
  emailOk,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  Pager,
  run,
  s,
  selectClass,
  StateBadge,
  Table,
  Time,
  useOffset,
  useParam,
  type Page,
  type Row,
} from "../ui";

// ---------------------------------------------------------------- mailboxes

export function Mailboxes() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const domains = useQuery({
    queryKey: key("receiving-domains"),
    queryFn: () => api.get<{ items: Row[] }>("/receiving-domains"),
  });
  const list = useQuery({
    queryKey: key("mailboxes"),
    queryFn: () => api.get<Page>("/mailboxes?limit=100"),
  });
  const noDomain = domains.isSuccess && domains.data.items.length === 0;
  return (
    <>
      <PageHeader
        title={phrase("Mailboxes")}
        description={phrase("Addresses that receive mail into the Inbox.")}
        action={
          noDomain ? (
            <Button asChild>
              <Link to={to("domains")}>{phrase("Connect a domain first")}</Link>
            </Button>
          ) : (
            <Button onClick={() => setOpen(true)} disabled={!domains.isSuccess}>
              <Plus size={16} /> {phrase("Create mailbox")}
            </Button>
          )
        }
      />
      {noDomain && (
        <p className="mb-4 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
          {phrase(
            "No receiving domain yet. Add a domain with purpose “Receiving” and verify it, or set INBOUND_DOMAIN on the server.",
          )}
        </p>
      )}
      <DataState
        q={list}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No mailboxes")}
              description={phrase(
                "Create an address such as help@ to receive replies and support mail.",
              )}
              action={
                noDomain ? (
                  <Button asChild>
                    <Link to={to("domains")}>{phrase("Connect a domain first")}</Link>
                  </Button>
                ) : (
                  <Button onClick={() => setOpen(true)}>{phrase("Create mailbox")}</Button>
                )
              }
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              {
                key: "address",
                label: phrase("Address"),
                render: (r) => (
                  <Link className="font-medium text-primary" to={to(`inbox?mailbox=${s(r.id)}`)}>
                    {r.catch_all ? `*@${s(r.domain)} (${phrase("catch-all")})` : s(r.address)}
                  </Link>
                ),
              },
              { key: "display_name", label: phrase("Name") },
              { key: "unread", label: phrase("Unread"), render: (r) => s(r.unread) },
              {
                key: "last",
                label: phrase("Last received"),
                render: (r) => <Time value={r.last_received_at} relative />,
              },
              {
                key: "enabled",
                label: phrase("State"),
                render: (r) => (
                  <StateBadge
                    state={r.enabled ? "active" : "cancelled"}
                    label={r.enabled ? phrase("enabled") : phrase("disabled")}
                  />
                ),
              },
              {
                key: "automations",
                label: phrase("Automations"),
                render: (r) => (
                  <Link className="text-primary" to={to(`automations?mailbox=${s(r.id)}`)}>
                    {s(r.automations)}
                  </Link>
                ),
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) => (
                  <span className="flex gap-1">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={async () => {
                        await run(
                          () => api.send(`/mailboxes/${s(r.id)}`, "PATCH", { enabled: !r.enabled }),
                          r.enabled ? phrase("Disabled.") : phrase("Enabled."),
                        );
                        client.invalidateQueries({ queryKey: key("mailboxes") });
                      }}
                    >
                      {r.enabled ? phrase("Disable") : phrase("Enable")}
                    </Button>
                    <ConfirmButton
                      title={`${phrase("Delete")} ${s(r.address)}?`}
                      description={`${phrase("The address stops receiving mail, and its")} ${s(r.messages)} ${phrase("stored messages and their attachments are deleted.")}`}
                      confirm="Delete mailbox and mail"
                      onConfirm={async () => {
                        await run(
                          () => api.send(`/mailboxes/${s(r.id)}`, "DELETE"),
                          phrase("Mailbox deleted."),
                        );
                        client.invalidateQueries({ queryKey: key("mailboxes") });
                      }}
                    >
                      <Trash2 size={14} />
                    </ConfirmButton>
                  </span>
                ),
              },
            ]}
          />
        )}
      </DataState>
      <MailboxDialog open={open} onOpenChange={setOpen} domains={domains.data?.items ?? []} />
    </>
  );
}

function MailboxDialog({
  open,
  onOpenChange,
  domains,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  domains: Row[];
}) {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [local, setLocal] = useState("");
  const [domain, setDomain] = useState("");
  const [name, setName] = useState("");
  const [catchAll, setCatchAll] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const dom = domain || s(domains[0]?.name);
  const localOk = catchAll || /^[a-z0-9]([a-z0-9._+-]{0,62}[a-z0-9])?$/.test(local);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("Create mailbox")}</DialogTitle>
        <DialogDescription>
          {phrase("Mail to this address appears in the Inbox.")}
        </DialogDescription>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!localOk)
              return setError(
                phrase("Use lowercase letters, digits, dot, dash, underscore or plus."),
              );
            setBusy(true);
            setError("");
            try {
              await api.send("/mailboxes", "POST", {
                local_part: local,
                domain: dom,
                display_name: name,
                catch_all: catchAll,
              });
              client.invalidateQueries({ queryKey: key("mailboxes") });
              client.invalidateQueries({ queryKey: key("checklist") });
              onOpenChange(false);
              navigate(to("mailboxes"));
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
            <Field label={phrase("Address")} error={error}>
              <input
                className={inputClass}
                disabled={catchAll}
                value={catchAll ? "*" : local}
                onChange={(e) => setLocal(e.target.value.toLowerCase())}
                placeholder="help"
              />
            </Field>
            <span className="pb-2">@</span>
            <Field label={phrase("Domain")}>
              <select
                className={selectClass}
                value={dom}
                onChange={(e) => setDomain(e.target.value)}
              >
                {domains.map((d) => (
                  <option key={s(d.name)} value={s(d.name)}>
                    {s(d.name)}
                  </option>
                ))}
              </select>
            </Field>
          </div>
          <Field label={phrase("Display name (optional)")}>
            <input
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Support"
            />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={catchAll}
              onChange={(e) => setCatchAll(e.target.checked)}
            />
            {phrase(
              "Catch-all: receive mail for every address on this domain not claimed by another mailbox",
            )}
          </label>
          <Button type="submit" disabled={busy || !dom}>
            {busy ? phrase("Creating…") : phrase("Create mailbox")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- senders

export function Senders() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const list = useQuery({
    queryKey: key("senders"),
    queryFn: () => api.get<Page>("/senders?limit=100"),
  });
  const domains = useQuery({
    queryKey: key("domains", "verified"),
    queryFn: () => api.get<Page>("/domains?limit=100"),
  });
  const sendingVerified = (domains.data?.items ?? []).filter(
    (d) => d.verified_at && d.purpose !== "receiving",
  );
  const none = domains.isSuccess && sendingVerified.length === 0;
  const anyDomain = domains.isSuccess && domains.data.items.length > 0;
  return (
    <>
      <PageHeader
        title={phrase("Sender addresses")}
        description={phrase(
          "Addresses this environment may send from. Compose and Bulk send preselect the default one.",
        )}
        action={
          none ? (
            <Button asChild>
              <Link to={to("domains")}>
                {anyDomain ? phrase("Verify a domain first") : phrase("Add a domain first")}
              </Link>
            </Button>
          ) : (
            <Button onClick={() => setOpen(true)} disabled={!domains.isSuccess}>
              <Plus size={16} /> {phrase("Add a sender")}
            </Button>
          )
        }
      />
      <DataState
        q={list}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No sender addresses")}
              description={
                none
                  ? phrase("Senders must be on a verified sending domain of this project.")
                  : phrase("Add the address your mail comes from, such as hello@ or no-reply@.")
              }
              action={
                none ? (
                  <Button asChild>
                    <Link to={to("domains")}>
                      {anyDomain ? phrase("Verify a domain first") : phrase("Add a domain first")}
                    </Link>
                  </Button>
                ) : (
                  <Button onClick={() => setOpen(true)}>{phrase("Add a sender")}</Button>
                )
              }
            />
          </div>
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              {
                key: "email",
                label: phrase("Address"),
                render: (r) => (
                  <span className="flex items-center gap-2">
                    {r.is_default ? (
                      <Star
                        size={14}
                        className="fill-accent text-accent"
                        aria-label={phrase("default")}
                      />
                    ) : null}
                    <Link
                      className="hover:underline"
                      to={to(`sent?sender=${encodeURIComponent(s(r.email))}`)}
                    >
                      {s(r.email)}
                    </Link>
                  </span>
                ),
              },
              { key: "display_name", label: phrase("Display name") },
              {
                key: "domain",
                label: phrase("Domain"),
                render: (r) =>
                  r.domain_id ? (
                    <Link className="text-primary" to={to(`domains/${s(r.domain_id)}`)}>
                      {s(r.domain)}
                    </Link>
                  ) : (
                    s(r.domain)
                  ),
              },
              {
                key: "status",
                label: phrase("Status"),
                render: (r) => (
                  <StateBadge
                    state={r.status === "verified" ? "verified" : "pending"}
                    label={s(r.status).replace(/_/g, " ")}
                  />
                ),
              },
              {
                key: "last_used_at",
                label: phrase("Last used"),
                render: (r) => <Time value={r.last_used_at} relative />,
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) => (
                  <span className="flex gap-1">
                    {!r.is_default && (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={async () => {
                          await run(
                            () => api.send(`/senders/${s(r.id)}/default`, "POST"),
                            phrase("Default sender set."),
                          );
                          client.invalidateQueries({ queryKey: key("senders") });
                          client.invalidateQueries({ queryKey: key("context") });
                        }}
                      >
                        {phrase("Make default")}
                      </Button>
                    )}
                    <ConfirmButton
                      title={`${phrase("Remove")} ${s(r.email)}?`}
                      description={phrase(
                        "Applications and Compose can no longer send from this address in this environment. Sent mail is kept.",
                      )}
                      confirm="Remove sender"
                      onConfirm={async () => {
                        await run(
                          () => api.send(`/senders/${s(r.id)}`, "DELETE"),
                          phrase("Sender removed."),
                        );
                        client.invalidateQueries({ queryKey: key("senders") });
                      }}
                    >
                      <Trash2 size={14} />
                    </ConfirmButton>
                  </span>
                ),
              },
            ]}
          />
        )}
      </DataState>
      <SenderDialog open={open} onOpenChange={setOpen} domains={sendingVerified} />
    </>
  );
}

function SenderDialog({
  open,
  onOpenChange,
  domains,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  domains: Row[];
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [local, setLocal] = useState("");
  const [domain, setDomain] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const dom = domain || s(domains[0]?.name);
  const email = `${local}@${dom}`;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("Add a sender")}</DialogTitle>
        <DialogDescription>
          {phrase("Only verified sending domains of this project are listed.")}
        </DialogDescription>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!emailOk(email)) return setError(phrase("Enter the part before @."));
            setBusy(true);
            setError("");
            try {
              await api.send("/senders", "POST", { email, display_name: name });
              client.invalidateQueries({ queryKey: key("senders") });
              client.invalidateQueries({ queryKey: key("checklist") });
              client.invalidateQueries({ queryKey: key("context") });
              onOpenChange(false);
              setLocal("");
              setName("");
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
            <Field label={phrase("Address")} error={error}>
              <input
                autoFocus
                className={inputClass}
                value={local}
                onChange={(e) => setLocal(e.target.value.toLowerCase())}
                placeholder="hello"
              />
            </Field>
            <span className="pb-2">@</span>
            <Field label={phrase("Domain")}>
              <select
                className={selectClass}
                value={dom}
                onChange={(e) => setDomain(e.target.value)}
              >
                {domains.map((d) => (
                  <option key={s(d.id)} value={s(d.name)}>
                    {s(d.name)}
                  </option>
                ))}
              </select>
            </Field>
          </div>
          <Field
            label={phrase("Display name")}
            hint={phrase("Shown to recipients, for example “Njiwa Support”.")}
          >
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <p className="text-xs text-muted-foreground">
            {phrase(
              "Your mail server must also allow the SMTP login to send as this address. See Delivery infrastructure.",
            )}
          </p>
          <Button type="submit" disabled={busy || !local}>
            {busy ? phrase("Adding…") : phrase("Add sender")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- suppressions

export function Suppressions() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [tab, setTab] = useParam("tab", "suppressions");
  const [source, setSource] = useParam("source");
  const [email, setEmail] = useState("");
  const [reason, setReason] = useState("");
  const [removeReason, setRemoveReason] = useState("");
  const [error, setError] = useState("");
  const offset = useOffset();
  const list = useQuery({
    queryKey: key("suppressions", source, offset),
    queryFn: () =>
      api.get<Page>(`/suppressions?limit=50&offset=${offset}${source ? `&source=${source}` : ""}`),
    enabled: tab === "suppressions",
  });
  const unsubs = useQuery({
    queryKey: key("unsubscribes", offset),
    queryFn: () => api.get<Page>(`/unsubscribes?limit=50&offset=${offset}`),
    enabled: tab === "unsubscribes",
  });
  return (
    <>
      <PageHeader
        title={phrase("Suppressions")}
        description={phrase(
          "Addresses this environment will not send to. Bulk unsubscribes affect announcements only.",
        )}
      />
      <div className="mb-4 flex gap-2 border-b" role="tablist">
        {[
          ["suppressions", "Suppressed"],
          ["unsubscribes", "Bulk unsubscribes"],
        ].map(([v, l]) => (
          <button
            key={v}
            role="tab"
            aria-selected={tab === v}
            onClick={() => setTab(v)}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${tab === v ? "border-primary font-semibold" : "border-transparent text-muted-foreground"}`}
          >
            {phrase(l)}
          </button>
        ))}
      </div>
      {tab === "suppressions" ? (
        <>
          <form
            className="mb-4 grid gap-3 rounded-xl border bg-card p-4 sm:grid-cols-[1fr_1fr_auto]"
            onSubmit={async (e) => {
              e.preventDefault();
              if (!emailOk(email)) return setError(phrase("Enter an email address."));
              if (!reason.trim()) return setError(phrase("Give a reason."));
              setError("");
              if (
                await run(
                  () => api.send("/suppressions", "POST", { email, reason }),
                  phrase("Address suppressed."),
                )
              ) {
                setEmail("");
                setReason("");
                client.invalidateQueries({ queryKey: key("suppressions") });
              }
            }}
          >
            <Field label={phrase("Email")} error={error}>
              <input
                className={inputClass}
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </Field>
            <Field label={phrase("Reason")}>
              <input
                className={inputClass}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder={phrase("Asked not to be contacted")}
              />
            </Field>
            <Button type="submit" className="self-end">
              {phrase("Suppress")}
            </Button>
          </form>
          <div className="mb-3 w-48">
            <Field label={phrase("Cause")}>
              <select
                className={selectClass}
                value={source}
                onChange={(e) => setSource(e.target.value)}
              >
                <option value="">{phrase("All")}</option>
                <option value="manual">{phrase("Added by hand")}</option>
                <option value="hard_bounce">{phrase("Hard bounce")}</option>
                <option value="complaint">{phrase("Complaint")}</option>
              </select>
            </Field>
          </div>
          <DataState
            q={list}
            isEmpty={(d) => d.items.length === 0}
            empty={
              <div className="rounded-xl border bg-card">
                <Empty
                  title={phrase("No suppressed addresses")}
                  description={phrase("Hard bounces and complaints are added automatically.")}
                />
              </div>
            }
          >
            {(d) => (
              <>
                <Table
                  rows={d.items}
                  rowKey={(r) => s(r.id)}
                  columns={[
                    { key: "recipient", label: phrase("Recipient") },
                    { key: "reason", label: phrase("Reason") },
                    {
                      key: "source",
                      label: phrase("Cause"),
                      render: (r) =>
                        r.bounce_message_id ? (
                          <Link
                            className="text-primary"
                            to={to(`messages/${s(r.bounce_message_id)}`)}
                          >
                            {s(r.source).replace("_", " ")} {s(r.bounce_status)}
                          </Link>
                        ) : (
                          s(r.source).replace("_", " ")
                        ),
                    },
                    {
                      key: "created_at",
                      label: phrase("Added"),
                      render: (r) => <Time value={r.created_at} />,
                    },
                    {
                      key: "x",
                      label: <span className="sr-only">{phrase("Actions")}</span>,
                      render: (r) => (
                        <ConfirmButton
                          title={`${phrase("Remove the suppression for")} ${s(r.recipient)}?`}
                          description={
                            <div className="space-y-2">
                              <p>
                                {phrase(
                                  "Mail to this address will be sent again. Only do this if the reason no longer applies.",
                                )}
                              </p>
                              <input
                                className={inputClass}
                                placeholder={phrase("Reason (required)")}
                                value={removeReason}
                                onChange={(e) => setRemoveReason(e.target.value)}
                              />
                            </div>
                          }
                          confirm="Remove suppression"
                          onConfirm={async () => {
                            if (!removeReason.trim()) {
                              await run(
                                () => Promise.reject(new Error(phrase("A reason is required."))),
                                "",
                              );
                              return;
                            }
                            await run(
                              () =>
                                api.send(`/suppressions/${s(r.id)}`, "DELETE", {
                                  reason: removeReason,
                                }),
                              phrase("Suppression removed."),
                            );
                            setRemoveReason("");
                            client.invalidateQueries({ queryKey: key("suppressions") });
                          }}
                        >
                          {phrase("Remove")}
                        </ConfirmButton>
                      ),
                    },
                  ]}
                />
                <Pager page={d} />
              </>
            )}
          </DataState>
        </>
      ) : (
        <DataState
          q={unsubs}
          isEmpty={(d) => d.items.length === 0}
          empty={<Empty title={phrase("No bulk unsubscribes")} />}
        >
          {(d) => (
            <>
              <Table
                rows={d.items}
                rowKey={(r) => s(r.recipient) + s(r.created_at)}
                columns={[
                  { key: "recipient", label: phrase("Recipient") },
                  {
                    key: "campaign",
                    label: phrase("From announcement"),
                    render: (r) =>
                      r.campaign_id ? (
                        <Link className="text-primary" to={to(`bulk/${s(r.campaign_id)}`)}>
                          {s(r.campaign)}
                        </Link>
                      ) : (
                        "—"
                      ),
                  },
                  {
                    key: "created_at",
                    label: phrase("When"),
                    render: (r) => <Time value={r.created_at} />,
                  },
                ]}
              />
              <Pager page={d} />
            </>
          )}
        </DataState>
      )}
    </>
  );
}

// ---------------------------------------------------------------- complaints

export function Complaints() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const offset = useOffset();
  const [type, setType] = useParam("type", "complaint");
  const q = useQuery({
    queryKey: key("bounces", type, offset),
    queryFn: () => api.get<Page>(`/bounces?type=${type}&limit=50&offset=${offset}`),
  });
  return (
    <>
      <PageHeader
        title={phrase("Complaints")}
        description={phrase(
          "Spam complaints from feedback loops, and bounces. A complaint suppresses the recipient and pauses any announcement it came from.",
        )}
      />
      <div className="mb-3 w-56">
        <Field label={phrase("Show")}>
          <select className={selectClass} value={type} onChange={(e) => setType(e.target.value)}>
            <option value="complaint">{phrase("Complaints")}</option>
            <option value="hard">{phrase("Hard bounces")}</option>
            <option value="soft">{phrase("Soft bounces")}</option>
            <option value="policy">{phrase("Policy bounces")}</option>
            <option value="unknown">{phrase("Unclassified")}</option>
          </select>
        </Field>
      </div>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("Nothing recorded")}
              description={phrase(
                "Complaints arrive when providers send feedback-loop reports to fbl@ on the bounce domain (see Mail server setup).",
              )}
            />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                { key: "recipient", label: phrase("Recipient") },
                { key: "type", label: phrase("Type") },
                { key: "status_code", label: phrase("Status") },
                { key: "diagnostic", label: phrase("Diagnostic") },
                {
                  key: "message",
                  label: phrase("Message"),
                  render: (r) =>
                    r.message_id ? (
                      <Link className="text-primary" to={to(`messages/${s(r.message_id)}`)}>
                        {phrase("Open")}
                      </Link>
                    ) : (
                      phrase("not matched")
                    ),
                },
                {
                  key: "suppression",
                  label: phrase("Suppressed"),
                  render: (r) =>
                    r.suppression_id ? (
                      <Link className="text-primary" to={to("suppressions")}>
                        {phrase("yes")}
                      </Link>
                    ) : (
                      phrase("no")
                    ),
                },
                {
                  key: "received_at",
                  label: phrase("Received"),
                  render: (r) => <Time value={r.received_at} />,
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
