import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Star } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import {
  ConfirmButton,
  DataState,
  Empty,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  run,
  s,
  selectClass,
  StateBadge,
  Table,
  Time,
  type Page,
  type Row,
} from "../ui";

export const purposeLabel: Record<string, string> = {
  transactional: "Transactional",
  marketing: "Marketing and newsletters",
  support: "Support",
  system: "System",
};

const domainState: Record<string, { label: string; why: string }> = {
  verified: { label: "Verified", why: "" },
  needs_dns: {
    label: "Needs DNS",
    why: "Live mail is blocked until the domain's DNS records are verified.",
  },
  claimed_elsewhere: {
    label: "Claimed elsewhere",
    why: "Live mail is blocked: another project holds this domain.",
  },
  domain_not_added: {
    label: "Domain not added",
    why: "Live mail is blocked: add the domain to this project and verify it.",
  },
};

export function Senders() {
  const phrase = usePhrase();
  const { api, to, project } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Row | null>(null);
  const [removing, setRemoving] = useState<Row | null>(null);
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
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("senders") });
    client.invalidateQueries({ queryKey: key("context") });
    client.invalidateQueries({ queryKey: key("checklist") });
  };
  const add = none ? (
    <Button asChild>
      <Link to={to("domains")}>{phrase("Verify a domain first")}</Link>
    </Button>
  ) : (
    <Button onClick={() => setAdding(true)} disabled={!domains.isSuccess}>
      <Plus size={16} /> {phrase("Add a sender")}
    </Button>
  );
  return (
    <>
      <PageHeader
        title={phrase("Sender addresses")}
        description={`${project}: ${phrase("the addresses this environment may send from. Each purpose has its own default.")}`}
        action={add}
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
              action={add}
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
                  <span className="block">
                    <span className="flex items-center gap-2">
                      {r.is_default ? (
                        <Star
                          size={14}
                          className="fill-warning text-warning"
                          aria-label={phrase("default")}
                        />
                      ) : null}
                      <Link
                        className="hover:underline"
                        to={to(`sent?sender=${encodeURIComponent(s(r.email))}`)}
                      >
                        {s(r.email)}
                      </Link>
                      {r.disabled_at ? (
                        <StateBadge state="cancelled" label={phrase("disabled")} />
                      ) : null}
                    </span>
                    {r.reply_to ? (
                      <span className="block text-xs text-muted-foreground">
                        {phrase("Reply-to")}: {s(r.reply_to)}
                      </span>
                    ) : null}
                  </span>
                ),
              },
              { key: "display_name", label: phrase("Display name") },
              {
                key: "purpose",
                label: phrase("Purpose"),
                render: (r) => phrase(purposeLabel[s(r.purpose)] ?? s(r.purpose)),
              },
              {
                key: "status",
                label: phrase("Domain"),
                render: (r) => {
                  const st = domainState[s(r.status)] ?? domainState.domain_not_added;
                  return (
                    <span className="block max-w-56">
                      {r.domain_id ? (
                        <Link className="text-primary" to={to(`domains/${s(r.domain_id)}`)}>
                          {s(r.domain)}
                        </Link>
                      ) : (
                        s(r.domain)
                      )}{" "}
                      <StateBadge
                        state={r.status === "verified" ? "verified" : "pending"}
                        label={phrase(st.label)}
                      />
                      {st.why && (
                        <span className="block text-xs text-muted-foreground">
                          {phrase(st.why)}
                        </span>
                      )}
                    </span>
                  );
                },
              },
              { key: "messages", label: phrase("Messages"), render: (r) => s(r.messages) },
              {
                key: "last_used_at",
                label: phrase("Last used"),
                render: (r) => <Time value={r.last_used_at} relative />,
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) => (
                  <span className="flex flex-wrap gap-1">
                    <Button size="sm" variant="outline" onClick={() => setEditing(r)}>
                      {phrase("Edit")}
                    </Button>
                    {!r.is_default && !r.disabled_at && (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={async () => {
                          await run(
                            () => api.send(`/senders/${s(r.id)}/default`, "POST"),
                            phrase("Default sender set."),
                          );
                          refresh();
                        }}
                      >
                        {phrase("Make default")}
                      </Button>
                    )}
                    <ConfirmButton
                      destructive={!r.disabled_at}
                      title={`${phrase(r.disabled_at ? "Enable" : "Disable")} ${s(r.email)}?`}
                      description={phrase(
                        r.disabled_at
                          ? "The address can send again."
                          : "Nothing can send from this address until it is enabled again. Its history is kept.",
                      )}
                      confirm={r.disabled_at ? "Enable" : "Disable"}
                      onConfirm={async () => {
                        await run(
                          () =>
                            api.send(`/senders/${s(r.id)}`, "PATCH", { disabled: !r.disabled_at }),
                          phrase("Saved."),
                        );
                        refresh();
                      }}
                    >
                      {phrase(r.disabled_at ? "Enable" : "Disable")}
                    </ConfirmButton>
                    <UsedBy sender={r} />
                    <Button size="sm" variant="outline" onClick={() => setRemoving(r)}>
                      {phrase("Delete")}
                    </Button>
                  </span>
                ),
              },
            ]}
          />
        )}
      </DataState>
      {adding && (
        <SenderForm domains={sendingVerified} onClose={() => setAdding(false)} onSaved={refresh} />
      )}
      {editing && (
        <SenderForm
          sender={editing}
          domains={sendingVerified}
          onClose={() => setEditing(null)}
          onSaved={refresh}
        />
      )}
      {removing && (
        <DeleteSender
          sender={removing}
          others={(list.data?.items ?? []).filter((x) => x.id !== removing.id && !x.disabled_at)}
          onClose={() => setRemoving(null)}
          onDeleted={refresh}
        />
      )}
    </>
  );
}

function SenderForm({
  sender,
  domains,
  onClose,
  onSaved,
}: {
  sender?: Row;
  domains: Row[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const [local, setLocal] = useState("");
  const [domain, setDomain] = useState("");
  const [name, setName] = useState(sender ? s(sender.display_name) : "");
  const [replyTo, setReplyTo] = useState(sender ? s(sender.reply_to) : "");
  const [purpose, setPurpose] = useState(sender ? s(sender.purpose) : "transactional");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const email = `${local}@${domain || s(domains[0]?.name)}`;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>
          {sender ? `${phrase("Edit")} ${s(sender.email)}` : phrase("Add a sender")}
        </DialogTitle>
        <DialogDescription>
          {phrase(
            sender
              ? "The address itself cannot change. Add another sender for a new address."
              : "Only verified sending domains of this project are listed.",
          )}
        </DialogDescription>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!sender && !/^[a-z0-9._+-]+$/.test(local))
              return setError(phrase("Enter the part before @."));
            setBusy(true);
            setError("");
            try {
              const body = { display_name: name, reply_to: replyTo, purpose };
              if (sender) await api.send(`/senders/${s(sender.id)}`, "PATCH", body);
              else await api.send("/senders", "POST", { email, ...body });
              onSaved();
              onClose();
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          {!sender && (
            <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2">
              <Field label={phrase("Address")}>
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
                  value={domain || s(domains[0]?.name)}
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
          )}
          <Field label={phrase("Display name")}>
            <input
              className={inputClass}
              value={name}
              maxLength={64}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field
            label={phrase("Reply-to")}
            hint={phrase("Optional: where replies go, when a message names no reply-to itself.")}
          >
            <input
              className={inputClass}
              type="email"
              value={replyTo}
              onChange={(e) => setReplyTo(e.target.value)}
            />
          </Field>
          <Field
            label={phrase("Purpose")}
            hint={phrase("Marketing senders must use a bulk domain.")}
          >
            <select
              className={selectClass}
              value={purpose}
              onChange={(e) => setPurpose(e.target.value)}
            >
              {Object.entries(purposeLabel).map(([v, l]) => (
                <option key={v} value={v}>
                  {phrase(l)}
                </option>
              ))}
            </select>
          </Field>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button type="submit" disabled={busy}>
            {phrase("Save")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

type Usage = Record<string, { id: string; name: string }[]>;
const usageLabel: [string, string][] = [
  ["templates", "Templates"],
  ["api_keys", "API keys"],
  ["campaigns", "Campaigns"],
  ["forms", "Forms"],
  ["chats", "Chats"],
  ["reports", "Report schedules"],
];

/** What uses this sender: templates, keys, campaigns, forms, chats and reports. */
function UsedBy({ sender }: { sender: Row }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const [open, setOpen] = useState(false);
  const usage = useQuery({
    queryKey: key("sender-usage", s(sender.id)),
    queryFn: () => api.get<Usage>(`/senders/${s(sender.id)}/usage`),
    enabled: open,
    staleTime: 0,
  });
  const used = usageLabel.filter(([k]) => (usage.data?.[k]?.length ?? 0) > 0);
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
        {phrase("Used by")}
      </Button>
      {open && (
        <Dialog open onOpenChange={setOpen}>
          <DialogContent>
            <DialogTitle>
              {phrase("Used by")}: {s(sender.email)}
            </DialogTitle>
            <DialogDescription>
              {phrase("What sends as this address in this environment.")}
            </DialogDescription>
            {usage.data && used.length === 0 && (
              <p className="text-sm">{phrase("Nothing uses this sender. Sent mail is kept.")}</p>
            )}
            <ul className="space-y-1 text-sm">
              {used.map(([k, label]) => (
                <li key={k}>
                  <span className="font-medium">{phrase(label)}:</span>{" "}
                  {usage.data![k].map((x) => x.name || x.id).join(", ")}
                </li>
              ))}
            </ul>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

/** Shows what uses a sender; deleting one in use needs a replacement. */
export function DeleteSender({
  sender,
  others,
  onClose,
  onDeleted,
}: {
  sender: Row;
  others: Row[];
  onClose: () => void;
  onDeleted: () => void;
}) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const [replacement, setReplacement] = useState("");
  const [error, setError] = useState("");
  const usage = useQuery({
    queryKey: key("sender-usage", s(sender.id)),
    queryFn: () => api.get<Usage>(`/senders/${s(sender.id)}/usage`),
    staleTime: 0,
  });
  const used = usageLabel.filter(([k]) => (usage.data?.[k]?.length ?? 0) > 0);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>
          {phrase("Delete")} {s(sender.email)}
        </DialogTitle>
        <DialogDescription>
          {phrase(
            used.length
              ? "This sender is in use. Choose a sender to take its place, or cancel."
              : "Nothing uses this sender. Sent mail is kept.",
          )}
        </DialogDescription>
        {used.length > 0 && (
          <ul className="space-y-1 text-sm" data-testid="sender-usage">
            {used.map(([k, label]) => (
              <li key={k}>
                <span className="font-medium">{phrase(label)}:</span>{" "}
                {usage.data![k].map((x) => x.name || x.id).join(", ")}
              </li>
            ))}
          </ul>
        )}
        {used.length > 0 && (
          <Field label={phrase("Replace with")}>
            <select
              className={selectClass}
              value={replacement}
              onChange={(e) => setReplacement(e.target.value)}
            >
              <option value="">{phrase("Choose a sender")}</option>
              {others.map((o) => (
                <option key={s(o.id)} value={s(o.id)}>
                  {s(o.email)}
                </option>
              ))}
            </select>
          </Field>
        )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>
            {phrase("Cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={!usage.data || (used.length > 0 && !replacement)}
            onClick={async () => {
              try {
                await api.send(
                  `/senders/${s(sender.id)}`,
                  "DELETE",
                  replacement ? { replace_with: replacement } : undefined,
                );
                onDeleted();
                onClose();
              } catch (err) {
                setError(errorMessage(err));
              }
            }}
          >
            {phrase("Delete sender")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** Checkboxes limiting an API key to chosen senders; none chosen means any. */
export function SenderChoice({
  value,
  onChange,
  senders,
}: {
  value: string[];
  onChange: (v: string[]) => void;
  senders: Row[];
}) {
  const phrase = usePhrase();
  if (!senders.length) return null;
  return (
    <fieldset className="space-y-1">
      <legend className="mb-1 text-sm font-medium">{phrase("Limit to senders")}</legend>
      <p className="text-xs text-muted-foreground">
        {phrase("Choose none to let the key send from any sender of this environment.")}
      </p>
      {senders.map((r) => (
        <label key={s(r.id)} className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={value.includes(s(r.email))}
            onChange={(e) =>
              onChange(
                e.target.checked ? [...value, s(r.email)] : value.filter((x) => x !== s(r.email)),
              )
            }
          />
          {s(r.email)}
        </label>
      ))}
    </fieldset>
  );
}

/** Edits the senders of an existing key. */
export function KeySendersButton({ prefix, current }: { prefix: string; current: string[] }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(current);
  const senders = useQuery({
    queryKey: key("senders"),
    queryFn: () => api.get<Page>("/senders?limit=100"),
    enabled: open,
  });
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
        {phrase("Senders")}
      </Button>
      {open && (
        <Dialog open onOpenChange={setOpen}>
          <DialogContent>
            <DialogTitle>
              {phrase("Senders of key")} {prefix}
            </DialogTitle>
            <DialogDescription>
              {phrase("A send from any other address is refused with sender_not_allowed.")}
            </DialogDescription>
            <SenderChoice value={value} onChange={setValue} senders={senders.data?.items ?? []} />
            <Button
              onClick={async () => {
                await run(
                  () => api.send(`/keys/${prefix}/senders`, "PUT", { senders: value }),
                  phrase("Saved."),
                );
                client.invalidateQueries({ queryKey: key("keys") });
                setOpen(false);
              }}
            >
              {phrase("Save")}
            </Button>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

/** The sender a project's template is sent from when a request names none. */
export function TemplateSender({ id, current }: { id: string; current: string }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [value, setValue] = useState(current);
  const senders = useQuery({
    queryKey: key("senders"),
    queryFn: () => api.get<Page>("/senders?limit=100"),
  });
  const items = senders.data?.items ?? [];
  return (
    <Field
      label={phrase("Default sender")}
      hint={phrase("Used when a send names no sender of its own.")}
    >
      <select
        className={selectClass}
        value={value}
        onChange={async (e) => {
          const email = e.target.value;
          const ok = await run(async () => {
            await api.send(`/templates/${id}/default-sender`, "PUT", { email });
            return true;
          }, phrase("Saved."));
          if (ok) setValue(email);
          client.invalidateQueries({ queryKey: key("templates") });
        }}
      >
        <option value="">{phrase("None: the request names the sender")}</option>
        {value && !items.some((r) => r.email === value) && <option value={value}>{value}</option>}
        {items.map((r) => (
          <option key={s(r.id)} value={s(r.email)}>
            {s(r.email)}
          </option>
        ))}
      </select>
    </Field>
  );
}
