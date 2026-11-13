import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Plus, RefreshCw, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { DnsSetup } from "./DnsSetup";
import { useWorkspace, useWsKey } from "../context";
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

type DnsRecord = {
  key: string;
  type: string;
  host: string;
  short_host: string;
  value: string;
  priority?: number;
  pass: boolean;
  found: string;
  hint?: string;
  /** The value is guidance with a part to fill in, not a publishable record. */
  placeholder?: boolean;
};

const recordName: Record<string, string> = {
  ownership: "Ownership",
  spf: "SPF",
  dkim: "DKIM",
  dmarc: "DMARC",
  mx: "MX (receiving)",
  return_path: "Return path",
};

function AddDomainDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState("sending");
  const [bulk, setBulk] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const valid = /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i.test(name.trim());
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("Add domain")}</DialogTitle>
        <DialogDescription>
          {phrase("You will get the DNS records to publish next.")}
        </DialogDescription>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!valid) return setError(phrase("Enter a domain such as notify.example.com"));
            setBusy(true);
            setError("");
            try {
              const r = await api.send<{ id: string }>("/domains", "POST", {
                name: name.trim().toLowerCase(),
                purpose,
                bulk,
              });
              client.invalidateQueries({ queryKey: key("domains") });
              onOpenChange(false);
              navigate(to(`domains/${r.id}`), { state: { created: true } });
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field
            label={phrase("Domain")}
            error={error}
            hint={phrase(
              "A subdomain such as notify.example.com keeps mail separate from your main domain.",
            )}
          >
            <input
              autoFocus
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="notify.example.com"
            />
          </Field>
          <Field label={phrase("Purpose")}>
            <select
              className={selectClass}
              value={purpose}
              onChange={(e) => setPurpose(e.target.value)}
            >
              <option value="sending">{phrase("Sending")}</option>
              <option value="receiving">{phrase("Receiving")}</option>
              <option value="both">{phrase("Sending and receiving")}</option>
            </select>
          </Field>
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-1"
              checked={bulk}
              onChange={(e) => setBulk(e.target.checked)}
            />
            <span>
              {phrase("Use only for bulk announcements")}
              <span className="block text-xs text-muted-foreground">
                {phrase(
                  "Bulk send requires its own subdomain (for example news.example.com) so problems there never affect transactional mail.",
                )}
              </span>
            </span>
          </label>
          <Button type="submit" disabled={busy || !valid}>
            {busy ? phrase("Adding…") : phrase("Add domain")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function domainStatus(d: Row) {
  if (!d.verified_at) return <StateBadge state="pending" label="ownership pending" />;
  if (d.needs_attention) return <StateBadge state="pending" label="records pending" />;
  return <StateBadge state="verified" />;
}

export function Domains() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const navigate = useNavigate();
  const [q, setQ] = useParam("q");
  const [attention, setAttention] = useParam("attention");
  const [search, setSearch] = useState(q);
  const [adding, setAdding] = useState(false);
  const offset = useOffset();
  const params = new URLSearchParams({ limit: "50", offset: String(offset) });
  if (q) params.set("q", q);
  if (attention) params.set("attention", "1");
  const list = useQuery({
    queryKey: key("domains", params.toString()),
    queryFn: () => api.get<Page>(`/domains?${params}`),
  });
  const okIcon = (v: unknown) =>
    v ? (
      <CheckCircle2 size={16} className="text-success" aria-label="pass" />
    ) : (
      <XCircle size={16} className="text-destructive" aria-label="fail" />
    );
  return (
    <>
      <PageHeader
        title={phrase("Domains")}
        description={
          <>
            {phrase("Domains this project sends from or receives on.")}{" "}
            <Link className="text-primary" to={to("dns-guide")}>
              {phrase("DNS setup guide")}
            </Link>
          </>
        }
        action={
          <Button onClick={() => setAdding(true)}>
            <Plus size={16} /> {phrase("Add domain")}
          </Button>
        }
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setQ(search.trim());
          }}
        >
          <Field label={phrase("Search")}>
            <input
              type="search"
              className={inputClass}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onBlur={() => setQ(search.trim())}
            />
          </Field>
        </form>
        <label className="flex items-center gap-2 pb-2 text-sm">
          <input
            type="checkbox"
            checked={!!attention}
            onChange={(e) => setAttention(e.target.checked ? "1" : "")}
          />{" "}
          {phrase("Needs attention")}
        </label>
      </div>
      <DataState
        q={list}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={q || attention ? phrase("No domain matches") : phrase("No domains yet")}
              description={phrase(
                "Add the domain you will send from. You will get the exact DNS records to publish.",
              )}
              action={<Button onClick={() => setAdding(true)}>{phrase("Add domain")}</Button>}
            />
          </div>
        }
      >
        {(d) => (
          <>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              onRow={(r) => navigate(to(`domains/${s(r.id)}`))}
              columns={[
                {
                  key: "name",
                  label: phrase("Domain"),
                  render: (r) => (
                    <Link className="font-medium hover:underline" to={to(`domains/${s(r.id)}`)}>
                      {s(r.name)}
                    </Link>
                  ),
                },
                {
                  key: "purpose",
                  label: phrase("Purpose"),
                  render: (r) => `${s(r.purpose)}${r.bulk ? " · bulk" : ""}`,
                },
                { key: "status", label: phrase("Status"), render: domainStatus },
                {
                  key: "spf",
                  label: "SPF",
                  render: (r) => (r.purpose === "receiving" ? "—" : okIcon(r.spf_ok)),
                },
                {
                  key: "dkim",
                  label: "DKIM",
                  render: (r) => (r.purpose === "receiving" ? "—" : okIcon(r.dkim_ok)),
                },
                {
                  key: "dmarc",
                  label: "DMARC",
                  render: (r) => (r.purpose === "receiving" ? "—" : okIcon(r.dmarc_ok)),
                },
                {
                  key: "mx",
                  label: "MX",
                  render: (r) => (r.purpose === "sending" ? "—" : okIcon(r.mx_ok)),
                },
                {
                  key: "checked",
                  label: phrase("Last checked"),
                  render: (r) => <Time value={r.last_checked_at} relative />,
                },
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
      <AddDomainDialog open={adding} onOpenChange={setAdding} />
    </>
  );
}

/**
 * D04: the domain's own return-path (bounce) domain. DMARC counts SPF only when
 * the envelope sender's domain is aligned with the From domain, so a domain from
 * another organisation than the installation's bounce domain needs its own.
 */
function ReturnPath({
  id,
  name,
  value,
  onSaved,
}: {
  id: string;
  name: string;
  value: string;
  onSaved: () => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  return (
    <Section className="mt-4" title={phrase("Return path (bounce domain)")}>
      <p className="mb-3 text-sm text-muted-foreground">
        {phrase(
          "Mail leaves with a bounce address on this domain so SPF aligns with your From domain for DMARC. Leave it empty to use the installation's bounce domain, which works when it shares your organisation's domain. It must be this domain or a subdomain, for example",
        )}{" "}
        <code>bounce.{name}</code>.
      </p>
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={async (e) => {
          e.preventDefault();
          const r = await run(
            () => api.send(`/domains/${id}/return-path`, "PUT", { bounce_domain: draft }),
            phrase("Saved. Publish the records below and run the check again."),
          );
          if (r) onSaved();
        }}
      >
        <Field label={phrase("Return-path domain")} className="min-w-64 flex-1">
          <input
            className={inputClass}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={`bounce.${name}`}
            autoComplete="off"
          />
        </Field>
        <Button type="submit" variant="outline" disabled={draft.trim() === value}>
          {phrase("Save")}
        </Button>
      </form>
      <p className="mt-2 text-xs text-muted-foreground">
        {phrase(
          "Saving withdraws this domain's SPF and DMARC pass until the check is run again, so Live mail pauses until the new records are published.",
        )}
      </p>
    </Section>
  );
}

export function DomainDetail() {
  const phrase = usePhrase();
  const { id = "" } = useParams();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [checking, setChecking] = useState(false);
  const [steps, setSteps] = useState<Row[] | null>(null);
  const q = useQuery({
    queryKey: key("domain", id),
    queryFn: () => api.get<Row>(`/domains/${id}`),
  });
  const records = (q.data?.records as DnsRecord[] | undefined) ?? [];
  const pending = q.data
    ? !!q.data.needs_attention ||
      !q.data.verified_at ||
      records.some((r) => !r.pass && r.key !== "return_path")
    : false;
  const check = async (quiet = false) => {
    setChecking(true);
    await run(
      () => api.send(`/domains/${id}/check`, "POST"),
      quiet ? "" : phrase("Records checked."),
    );
    setChecking(false);
    client.invalidateQueries({ queryKey: key("domain", id) });
    client.invalidateQueries({ queryKey: key("domains") });
    client.invalidateQueries({ queryKey: key("checklist") });
  };
  // Re-check automatically every minute while records are pending and the page is open.
  useEffect(() => {
    if (!pending) return;
    const t = setInterval(() => void check(true), 60_000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pending, id]);
  return (
    <DataState q={q}>
      {(d) => {
        const senders = (d.senders as Row[]) ?? [];
        const mailboxes = (d.mailboxes as Row[]) ?? [];
        const recent = (d.recent_messages as Row[]) ?? [];
        return (
          <>
            <PageHeader
              back={{ to: to("domains"), label: phrase("Domains") }}
              title={s(d.name)}
              description={
                <>
                  {s(d.purpose)}
                  {d.bulk ? ` · ${phrase("bulk only")}` : ""} · {phrase("last checked")}{" "}
                  <Time value={d.last_checked_at} relative />
                  {pending && ` · ${phrase("re-checking every minute while this page is open")}`}
                </>
              }
              action={
                <>
                  <Button onClick={() => void check()} disabled={checking}>
                    <RefreshCw size={15} className={checking ? "animate-spin" : ""} />{" "}
                    {phrase("Check now")}
                  </Button>
                  <ConfirmButton
                    size="default"
                    title={phrase("Delete this domain?")}
                    description={
                      senders.length + mailboxes.length > 0
                        ? `${phrase("It is used by")} ${[...senders.map((x) => s(x.email)), ...mailboxes.map((x) => s(x.address))].join(", ")}. ${phrase("Remove those first.")}`
                        : phrase("Its DNS check history stays; live sending from it stops.")
                    }
                    confirm="Delete domain"
                    disabled={senders.length + mailboxes.length > 0}
                    onConfirm={async () => {
                      if (
                        await run(
                          () => api.send(`/domains/${id}`, "DELETE"),
                          phrase("Domain deleted."),
                        )
                      ) {
                        client.invalidateQueries({ queryKey: key("domains") });
                        navigate(to("domains"));
                      }
                    }}
                  >
                    {phrase("Delete")}
                  </ConfirmButton>
                </>
              }
            />
            <DnsSetup
              domainId={id}
              domain={s(d.name)}
              onChanged={() => client.invalidateQueries({ queryKey: key("domain", id) })}
            />
            <Section
              title={phrase("DNS records")}
              action={
                <Link className="text-sm text-primary" to={to("dns-guide")}>
                  {phrase("How to publish these")}
                </Link>
              }
            >
              {d.requirements_changed === true && (
                <p
                  role="status"
                  className="mb-3 rounded-lg border border-warning/40 bg-warning/15 p-3 text-sm"
                >
                  {phrase(
                    "Delivery settings changed since the last check, so these are the records needed now. Publish any that changed, then run Check now.",
                  )}
                </p>
              )}
              <ul className="space-y-3">
                {records.map((r) => (
                  <li key={r.key} className="rounded-lg border p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      {r.pass ? (
                        <span role="img" aria-label={phrase("Passed")} className="inline-flex">
                          <CheckCircle2 size={18} className="text-success" aria-hidden="true" />
                        </span>
                      ) : (
                        <span role="img" aria-label={phrase("Not passing")} className="inline-flex">
                          <XCircle size={18} className="text-destructive" aria-hidden="true" />
                        </span>
                      )}
                      <span className="font-semibold">{phrase(recordName[r.key] ?? r.key)}</span>
                      <span className="rounded bg-muted px-1.5 text-xs">{r.type}</span>
                      {!r.pass && (
                        <Link
                          className="ml-auto text-xs text-primary"
                          to={to(`dns-guide#${r.key}`)}
                        >
                          {phrase("Help with this record")}
                        </Link>
                      )}
                    </div>
                    <dl className="mt-2 grid gap-2 text-sm sm:grid-cols-[8rem_1fr]">
                      <dt className="text-muted-foreground">{phrase("Host")}</dt>
                      <dd className="flex flex-wrap items-center gap-2">
                        <code className="break-all">{r.short_host}</code>
                        <CopyButton text={r.short_host} label="short host" />
                        <span className="text-xs text-muted-foreground">
                          {phrase("full name")} <code className="break-all">{r.host}</code>
                        </span>
                        <CopyButton text={r.host} label="full host" />
                      </dd>
                      <dt className="text-muted-foreground">{phrase("Value")}</dt>
                      {r.placeholder ? (
                        <dd data-placeholder="true">
                          <code className="break-all italic text-muted-foreground">
                            {r.priority ? `${r.priority} ` : ""}
                            {r.value}
                          </code>
                          <span className="mt-1 block text-xs text-warning">
                            {phrase(
                              "Template, not a record to copy: replace the part in <…> with your mail server's details before publishing.",
                            )}
                          </span>
                        </dd>
                      ) : (
                        <dd className="flex items-start gap-2">
                          <code className="break-all">
                            {r.priority ? `${r.priority} ` : ""}
                            {r.value}
                          </code>
                          <CopyButton text={r.value} label="value" />
                        </dd>
                      )}
                      <dt className="text-muted-foreground">{phrase("Found")}</dt>
                      <dd className="break-all text-xs">{r.found}</dd>
                    </dl>
                    {r.hint && (
                      <p className="mt-2 text-xs text-muted-foreground">{phrase(r.hint)}</p>
                    )}
                  </li>
                ))}
              </ul>
            </Section>
            {d.managed_key_missing === true && (
              <p role="status" className="mt-4 rounded-lg border p-3 text-sm">
                {phrase(
                  "This domain has no DKIM key on your mail host yet. Run the installer again (Admin → Mail host shows the exact command), then publish the DKIM record it prints.",
                )}
              </p>
            )}
            {d.purpose !== "receiving" && (
              <ReturnPath
                id={id ?? ""}
                name={s(d.name)}
                value={s(d.bounce_domain)}
                onSaved={() => {
                  client.invalidateQueries({ queryKey: key("domain", id) });
                  client.invalidateQueries({ queryKey: key("domains") });
                }}
              />
            )}
            {d.purpose !== "receiving" &&
              d.provider_type === "generic" &&
              s(d.sending_profile_id) && (
                <ServerDKIMKey
                  d={d}
                  onSaved={() => client.invalidateQueries({ queryKey: key("domain", id) })}
                />
              )}
            {d.purpose !== "receiving" && <SignedProof id={id ?? ""} d={d} />}
            {d.provider_type === "mailcow" ? (
              <Section className="mt-4" title={phrase("Mailcow")}>
                {d.mailcow_api ? (
                  <>
                    <p className="text-sm text-muted-foreground">
                      {phrase(
                        "Add this domain to Mailcow, create its DKIM key and (for receiving) the relay transport, through the Mailcow API.",
                      )}
                      {d.mailcow_provisioned_at ? (
                        <>
                          {" "}
                          {phrase("Done")} <Time value={d.mailcow_provisioned_at} />.
                        </>
                      ) : null}
                    </p>
                    <Button
                      className="mt-3"
                      variant="outline"
                      onClick={async () => {
                        const r = await run(
                          () => api.send<{ steps: Row[] }>(`/domains/${id}/mailcow`, "POST"),
                          "",
                        );
                        if (r) setSteps(r.steps);
                        client.invalidateQueries({ queryKey: key("domain", id) });
                      }}
                    >
                      {phrase("Set up in Mailcow")}
                    </Button>
                    {steps && (
                      <ul className="mt-3 space-y-1 text-sm">
                        {steps.map((st) => (
                          <li key={s(st.key)} className="flex items-center gap-2">
                            {st.ok ? (
                              <CheckCircle2 size={15} className="text-success" />
                            ) : (
                              <XCircle size={15} className="text-destructive" />
                            )}
                            {s(st.title)}{" "}
                            {st.detail ? (
                              <span className="text-muted-foreground">— {s(st.detail)}</span>
                            ) : null}
                          </li>
                        ))}
                      </ul>
                    )}
                  </>
                ) : (
                  <div className="text-sm">
                    <p className="text-muted-foreground">
                      {phrase(
                        "The Mailcow API is not configured, so do these steps in the Mailcow admin panel:",
                      )}
                    </p>
                    <ol className="mt-2 list-decimal space-y-1 pl-5">
                      <li>
                        {phrase("Configuration → Mail setup → Domains → Add domain:")}{" "}
                        <code>{s(d.name)}</code>
                        {d.purpose === "receiving" && ` ${phrase("(tick “Relay all recipients”)")}`}
                      </li>
                      {d.purpose !== "receiving" && (
                        <li>
                          {phrase("Configuration → ARC/DKIM keys → add a 2048-bit key for")}{" "}
                          <code>{s(d.name)}</code> {phrase("with selector")}{" "}
                          <code>{s(d.dkim_selector)}</code>
                          {phrase(", then publish the key as the DKIM record above.")}
                        </li>
                      )}
                      {d.purpose !== "sending" && (
                        <li>
                          {phrase("Configuration → Routing → Transport maps → add destination")}{" "}
                          <code>{s(d.name)}</code>{" "}
                          {phrase("with the Njiwa-Mail inbound listener as next hop.")}
                        </li>
                      )}
                    </ol>
                    <Link className="mt-2 inline-block text-primary" to={to("mail-server")}>
                      {phrase("Configure the Mailcow API on the Mail server page")}
                    </Link>
                  </div>
                )}
              </Section>
            ) : (
              <Section className="mt-4" title={phrase("Your mail server")}>
                <div className="text-sm">
                  <p className="text-muted-foreground">
                    {phrase(
                      "Njiwa-Mail checks DNS. Your mail server must also be set up for this domain:",
                    )}
                  </p>
                  <ol className="mt-2 list-decimal space-y-1 pl-5">
                    {d.purpose !== "receiving" && (
                      <li>
                        {phrase(
                          "Allow the SMTP login to send as addresses on this domain, and have it sign them with DKIM. Publish the DKIM key your mail server gives you as the DKIM record above.",
                        )}
                      </li>
                    )}
                    {d.purpose !== "receiving" && (
                      <li>
                        {phrase(
                          "The SPF record must list the server that actually sends the mail (your mail server), not the server Njiwa-Mail runs on, unless they are the same.",
                        )}
                      </li>
                    )}
                    {d.purpose !== "sending" && (
                      <li>
                        {phrase(
                          "For receiving, have your mail server relay this domain's mail to the Njiwa-Mail inbound listener.",
                        )}
                      </li>
                    )}
                  </ol>
                </div>
              </Section>
            )}
            <div className="mt-4 grid gap-4 lg:grid-cols-3">
              <Section
                title={phrase("Senders")}
                action={
                  <Link className="text-sm text-primary" to={to("senders")}>
                    {phrase("Manage")}
                  </Link>
                }
              >
                {senders.length ? (
                  <ul className="space-y-1 text-sm">
                    {senders.map((x) => (
                      <li key={s(x.id)}>
                        {s(x.email)}{" "}
                        <span className="text-xs text-muted-foreground">({s(x.mode)})</span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    {d.verified_at
                      ? phrase("No sender on this domain yet.")
                      : phrase("Verify ownership first, then add senders.")}
                  </p>
                )}
              </Section>
              <Section
                title={phrase("Mailboxes")}
                action={
                  <Link className="text-sm text-primary" to={to("mailboxes")}>
                    {phrase("Manage")}
                  </Link>
                }
              >
                {mailboxes.length ? (
                  <ul className="space-y-1 text-sm">
                    {mailboxes.map((x) => (
                      <li key={s(x.id)}>
                        <Link className="text-primary" to={to(`inbox?mailbox=${s(x.id)}`)}>
                          {s(x.address)}
                        </Link>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    {d.purpose === "sending"
                      ? phrase("This domain is for sending only.")
                      : phrase("No mailbox yet.")}
                  </p>
                )}
              </Section>
              <Section title={phrase("Recent messages")}>
                {recent.length ? (
                  <ul className="space-y-1 text-sm">
                    {recent.map((m) => (
                      <li key={s(m.id)} className="flex items-center gap-2">
                        <StateBadge state={s(m.state)} />
                        <Link className="truncate hover:underline" to={to(`messages/${s(m.id)}`)}>
                          {s(m.subject) || s(m.recipient)}
                        </Link>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    {phrase("No messages from this domain in this environment.")}
                  </p>
                )}
              </Section>
            </div>
          </>
        );
      }}
    </DataState>
  );
}

/**
 * For an existing SMTP server: the public key it signs this domain with, as
 * its operator gives it, so the DKIM check compares the published record with
 * the real key instead of a template (C07).
 */
function ServerDKIMKey({ d, onSaved }: { d: Row; onSaved: () => void }) {
  const phrase = usePhrase();
  const [val, setVal] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <Section className="mt-4" title={phrase("Your mail server's DKIM key for this domain")}>
      <p className="mb-2 text-xs text-muted-foreground">
        {phrase(
          "Paste the DKIM public key your mail server signs this domain with (its value starts with v=DKIM1). The DKIM check then compares the published record with it. Selector:",
        )}{" "}
        <code>{s(d.dkim_selector)}</code>
      </p>
      <textarea
        aria-label={phrase("DKIM public key")}
        className="h-20 w-full rounded-md border border-input bg-background p-2 font-mono text-xs"
        value={val}
        onChange={(e) => setVal(e.target.value)}
        placeholder="v=DKIM1; k=rsa; p=MIIBIjANBg…"
      />
      <Button
        className="mt-2"
        variant="outline"
        disabled={busy || !val.trim()}
        onClick={async () => {
          setBusy(true);
          const r = await run(
            () =>
              admin.send(
                `/installation/transport-profiles/${s(d.sending_profile_id)}/sending-facts`,
                "PUT",
                {
                  dkim_keys: { [s(d.name)]: val.trim() },
                },
              ),
            phrase("Key saved. Run Check now to compare it with DNS."),
          );
          setBusy(false);
          if (r) {
            setVal("");
            onSaved();
          }
        }}
      >
        {busy ? phrase("Saving…") : phrase("Save key")}
      </Button>
    </Section>
  );
}

type SignedCheck = {
  from_domain: string;
  aligned: boolean;
  recorded: boolean;
  message: string;
  signatures: { d: string; s: string; result: string; reason?: string }[];
};

/**
 * A published DKIM key only shows the key exists. This section records proof
 * that mail is actually signed: a received message with a valid signature
 * aligned with the domain (D06).
 */
function SignedProof({ id, d }: { id: string; d: Row }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [raw, setRaw] = useState("");
  const [busy, setBusy] = useState(false);
  const [res, setRes] = useState<SignedCheck | null>(null);
  return (
    <Section className="mt-4" title={phrase("Proof that mail is signed")}>
      <p className="text-sm" data-testid="dkim-proof">
        {d.dkim_verified_at ? (
          <>
            <StateBadge state="pass" label={phrase("Signed mail verified")} />{" "}
            {phrase("A received message carried a valid DKIM signature for this domain")} (s=
            {s(d.dkim_verified_selector)}), <Time value={d.dkim_verified_at} relative />.
          </>
        ) : (
          <>
            <StateBadge state="pending" label={phrase("Not proven yet")} />{" "}
            {phrase(
              "A published DKIM record does not prove your mail is signed. Send a test to your own mailbox, open the original message (Show original / View source) and paste it here.",
            )}
          </>
        )}
      </p>
      <textarea
        aria-label={phrase("Received message, headers included")}
        className="mt-3 h-32 w-full rounded-md border border-input bg-background p-2 font-mono text-xs"
        value={raw}
        onChange={(e) => setRaw(e.target.value)}
        placeholder="Received: …\nDKIM-Signature: v=1; a=rsa-sha256; …\nFrom: …"
      />
      <Button
        className="mt-2"
        variant="outline"
        disabled={busy || !raw.trim()}
        onClick={async () => {
          setBusy(true);
          const r = await run(
            () => api.send<SignedCheck>(`/domains/${id}/verify-message`, "POST", { raw }),
            "",
          );
          setBusy(false);
          if (r) {
            setRes(r);
            if (r.recorded) client.invalidateQueries({ queryKey: key("domain", id) });
          }
        }}
      >
        {busy ? phrase("Checking…") : phrase("Check signature")}
      </Button>
      {res && (
        <div role="status" className="mt-3 text-sm">
          <p>
            <StateBadge
              state={res.aligned ? "pass" : "fail"}
              label={res.aligned ? phrase("Verified") : phrase("Not verified")}
            />{" "}
            {phrase(res.message)}
          </p>
          {res.signatures.length > 0 && (
            <ul className="mt-1 space-y-1 text-xs text-muted-foreground">
              {res.signatures.map((sig, i) => (
                <li key={i}>
                  {sig.result} · d={sig.d} · s={sig.s}
                  {sig.reason ? ` · ${sig.reason}` : ""}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Section>
  );
}

export function DnsGuide() {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const sections: [string, string, string[]][] = [
    [
      "ownership",
      "Ownership TXT",
      [
        "Proves to Njiwa-Mail that the domain is yours. Publish it as a TXT record on the domain itself (host “@” or the subdomain label).",
        "It does not affect mail. You can leave it in place.",
      ],
    ],
    [
      "dkim",
      "DKIM",
      [
        "A public key that lets receivers check that mail really came from your server and was not changed on the way.",
        "The server that signs your mail creates the key (with Mailcow: Configuration → ARC/DKIM keys). Publish it as a TXT record at selector._domainkey under your domain.",
        "Paste the whole value. If your DNS panel splits it into several quoted pieces, that is fine; if it cuts it off, the check fails.",
      ],
    ],
    [
      "spf",
      "SPF",
      [
        "Lists which servers may send mail for the domain. It is a TXT record that starts with v=spf1.",
        "A domain must have only one SPF record. If one exists already (for example for Google Workspace), edit it and add the server that sends your mail, such as “mx” or “ip4:203.0.113.10”, before the final -all. Never publish a second one.",
      ],
    ],
    [
      "dmarc",
      "DMARC",
      [
        "Tells receivers what to do when SPF and DKIM fail, and where to send reports. It is a TXT record at _dmarc under your domain.",
        "Start with p=none, read the reports for a week or two, then move to p=quarantine.",
      ],
    ],
    [
      "mx",
      "MX (receiving)",
      [
        "Tells the world which server receives mail for the domain. For a receiving domain, point it at the mail hostname shown in the MX record on the domain's page, with priority 10: the check passes only when the MX target is exactly that host.",
        "Only add MX to a domain you want to receive on. Changing the MX of your main domain moves all of its mail.",
      ],
    ],
    [
      "return_path",
      "Return path",
      [
        "Bounces come back to the bounce address Njiwa-Mail uses as the envelope sender. Its domain needs an MX record, priority 10, pointing at the mail hostname shown in the Return path record on the domain's page; that server relays bounces@ and fbl@ to Njiwa-Mail. Do not change your main domain’s MX for this.",
      ],
    ],
  ];
  const providers: [string, string][] = [
    [
      "Cloudflare",
      "DNS → Records → Add record. Mail hostnames (the MX target, and any A record for the mail server) must be “DNS only” (grey cloud), never “Proxied”: Cloudflare does not proxy SMTP.",
    ],
    [
      "Namecheap",
      "Domain List → Manage → Advanced DNS → Add new record. Use the short host label (for example “_dmarc.notify”), not the full name.",
    ],
    ["GoDaddy", "My Products → DNS → Add. Use the short host label; GoDaddy appends the domain."],
    [
      "Route 53",
      "Hosted zones → Create record. Enter the label in “Record name”; put TXT values in double quotes and split values longer than 255 characters into several quoted strings.",
    ],
    [
      "Tanzanian registrars (TZNIC resellers)",
      "Records are usually edited in the reseller's DNS panel; if the panel has no TXT support, move DNS hosting to Cloudflare and keep the .tz registration.",
    ],
  ];
  const trouble: [string, string][] = [
    [
      "Record not found yet",
      "DNS changes can take from minutes to a few hours. The domain page re-checks every minute while it is open, and the server re-checks pending domains every 10 minutes.",
    ],
    [
      "Wrong host",
      "Many panels add the domain for you. If you typed the full name, the record ends up at notify.example.com.example.com. Use the short label shown next to each record.",
    ],
    [
      "Value cut off",
      "Long DKIM keys get truncated by some panels. Compare the “Found” value with the expected value; it must end with the same characters.",
    ],
    [
      "Two SPF records",
      "Receivers treat two SPF records as an error. Merge them into one record with all the mechanisms.",
    ],
    [
      "Cloudflare proxy",
      "An orange-cloud (proxied) record for the mail host breaks SMTP. Switch it to DNS only.",
    ],
  ];
  return (
    <>
      <PageHeader
        title={phrase("DNS setup guide")}
        description={phrase(
          "What each record does, how to publish it, and what to do when a check fails.",
        )}
        action={
          <Button asChild variant="outline">
            <Link to={to("domains")}>{phrase("Back to Domains")}</Link>
          </Button>
        }
      />
      <div className="space-y-4">
        {sections.map(([id, title, paras]) => (
          <Section key={id} title={<span id={id}>{phrase(title)}</span>}>
            {paras.map((p) => (
              <p key={p} className="mb-2 text-sm leading-relaxed">
                {phrase(p)}
              </p>
            ))}
          </Section>
        ))}
        <Section title={phrase("Common DNS providers")}>
          <dl className="space-y-3 text-sm">
            {providers.map(([name, text]) => (
              <div key={name}>
                <dt className="font-medium">{name}</dt>
                <dd className="text-muted-foreground">{phrase(text)}</dd>
              </div>
            ))}
          </dl>
        </Section>
        <Section title={phrase("Troubleshooting")}>
          <dl className="space-y-3 text-sm">
            {trouble.map(([name, text]) => (
              <div key={name}>
                <dt className="font-medium">{phrase(name)}</dt>
                <dd className="text-muted-foreground">{phrase(text)}</dd>
              </div>
            ))}
          </dl>
        </Section>
      </div>
    </>
  );
}
