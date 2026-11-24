import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
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
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  Time,
  useParam,
  type Page,
  type Row,
} from "../ui";

const actions: [string, string][] = [
  ["auto_reply", "Auto-reply from a template"],
  ["forward", "Forward to an email address"],
  ["webhook", "Call a webhook"],
  ["label", "Add a label"],
  ["archive", "Archive"],
];

function describe(r: Row, phrase: (t: string) => string) {
  const cond = [
    r.cond_sender && `${phrase("sender contains")} “${s(r.cond_sender)}”`,
    r.cond_subject && `${phrase("subject contains")} “${s(r.cond_subject)}”`,
    r.cond_has_attachment && phrase("has an attachment"),
  ].filter(Boolean);
  const act =
    r.action === "auto_reply"
      ? `${phrase("auto-reply with")} ${s(r.template)}`
      : r.action === "forward"
        ? `${phrase("forward to")} ${s(r.target)}`
        : r.action === "label"
          ? `${phrase("label")} “${s(r.target)}”`
          : r.action === "webhook"
            ? phrase("call a webhook")
            : phrase("archive");
  return `${phrase("When mail arrives at")} ${s(r.mailbox) || phrase("any mailbox")}${cond.length ? ` ${phrase("and")} ${cond.join(` ${phrase("and")} `)}` : ""}: ${act}.`;
}

export function Automations() {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const [mailboxFilter] = useParam("mailbox");
  const [openRule, setOpenRule] = useParam("rule");
  const [editing, setEditing] = useState<Row | null>(null);
  const [creating, setCreating] = useState(false);
  const q = useQuery({
    queryKey: key("automations"),
    queryFn: () => api.get<{ items: Row[] }>("/automations"),
  });
  const refresh = () => client.invalidateQueries({ queryKey: key("automations") });
  const move = async (items: Row[], i: number, dir: -1 | 1) => {
    const ids = items.map((r) => s(r.id));
    [ids[i], ids[i + dir]] = [ids[i + dir], ids[i]];
    await run(() => api.send("/automations/reorder", "POST", { ids }), phrase("Order saved."));
    refresh();
  };
  return (
    <>
      <PageHeader
        title={phrase("Automations")}
        description={phrase(
          "Rules that act on incoming mail, in order. Auto-replies never answer automatic mail and go at most once per sender per day.",
        )}
        action={
          <Button onClick={() => setCreating(true)}>
            <Plus size={16} /> {phrase("New rule")}
          </Button>
        }
      />
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No rules yet")}
              description={phrase(
                "For example: auto-reply to mail at help@ with a “we received your message” template.",
              )}
              action={<Button onClick={() => setCreating(true)}>{phrase("New rule")}</Button>}
            />
          </div>
        }
      >
        {(d) => {
          const items = d.items.filter(
            (r) => !mailboxFilter || r.mailbox_id === mailboxFilter || !r.mailbox_id,
          );
          return (
            <ol className="space-y-3">
              {items.map((r, i) => {
                const last = r.last_run as Row | null;
                return (
                  <li key={s(r.id)} className="rounded-xl border bg-card p-4">
                    <div className="flex flex-wrap items-center gap-3">
                      <span className="grid h-7 w-7 place-items-center rounded-full bg-muted text-xs font-bold">
                        {i + 1}
                      </span>
                      <span className="font-semibold">{s(r.name)}</span>
                      <Switch
                        checked={!!r.enabled}
                        aria-label={`${phrase("Turn on")} ${s(r.name)}`}
                        onCheckedChange={async (v) => {
                          await run(
                            () => api.send(`/automations/${s(r.id)}`, "PATCH", { enabled: v }),
                            v ? phrase("Rule turned on.") : phrase("Rule turned off."),
                          );
                          refresh();
                        }}
                      />
                      <span className="ml-auto flex gap-1">
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={phrase("Move up")}
                          disabled={i === 0 || !!mailboxFilter}
                          onClick={() => move(d.items, i, -1)}
                        >
                          <ArrowUp size={15} />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={phrase("Move down")}
                          disabled={i === items.length - 1 || !!mailboxFilter}
                          onClick={() => move(d.items, i, 1)}
                        >
                          <ArrowDown size={15} />
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => setEditing(r)}>
                          {phrase("Edit")}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setOpenRule(openRule === s(r.id) ? "" : s(r.id))}
                        >
                          {openRule === s(r.id) ? phrase("Hide") : phrase("Test and history")}
                        </Button>
                        <ConfirmButton
                          title={`${phrase("Delete rule")} “${s(r.name)}”?`}
                          description={phrase("The rule and its run history are deleted.")}
                          confirm="Delete rule"
                          onConfirm={async () => {
                            await run(
                              () => api.send(`/automations/${s(r.id)}`, "DELETE"),
                              phrase("Rule deleted."),
                            );
                            refresh();
                          }}
                        >
                          <Trash2 size={14} />
                        </ConfirmButton>
                      </span>
                    </div>
                    <p className="mt-2 text-sm text-muted-foreground">{describe(r, phrase)}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {s(r.runs)} {phrase("runs")}
                      {last ? (
                        <>
                          {" · "}
                          {phrase("last")}: {s(last.result)} — {s(last.detail)}{" "}
                          <Time value={last.at} relative />
                        </>
                      ) : null}
                    </p>
                    {openRule === s(r.id) && <RulePanel rule={r} />}
                  </li>
                );
              })}
            </ol>
          );
        }}
      </DataState>
      {(creating || editing) && (
        <RuleDialog
          rule={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
    </>
  );
}

function RulePanel({ rule }: { rule: Row }) {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const id = s(rule.id);
  const recent = useQuery({
    queryKey: key("inbox", "recent", s(rule.mailbox_id)),
    queryFn: () =>
      api.get<Page>(`/inbox?limit=10${rule.mailbox_id ? `&mailbox=${s(rule.mailbox_id)}` : ""}`),
  });
  const runs = useQuery({
    queryKey: key("automation-runs", id),
    queryFn: () => api.get<Page>(`/automations/${id}/runs?limit=20`),
  });
  const [msg, setMsg] = useState("");
  const [result, setResult] = useState<Row | null>(null);
  return (
    <div className="mt-4 grid gap-4 border-t pt-4 lg:grid-cols-2">
      <div>
        <p className="mb-2 text-sm font-medium">{phrase("Test against a recent message")}</p>
        <p className="mb-2 text-xs text-muted-foreground">
          {phrase("Nothing is sent or changed; the result is recorded as a test run.")}
        </p>
        <div className="flex gap-2">
          <select
            aria-label={phrase("Message")}
            className={selectClass}
            value={msg}
            onChange={(e) => setMsg(e.target.value)}
          >
            <option value="">
              {recent.data?.items.length ? phrase("Choose a message") : phrase("No recent mail")}
            </option>
            {recent.data?.items.map((m) => (
              <option key={s(m.id)} value={s(m.id)}>
                {s(m.header_from)} — {s(m.subject)}
              </option>
            ))}
          </select>
          <Button
            disabled={!msg}
            onClick={async () => {
              const r = await run(
                () => api.send<Row>(`/automations/${id}/test`, "POST", { inbound_id: msg }),
                "",
              );
              if (r) setResult(r);
              client.invalidateQueries({ queryKey: key("automation-runs", id) });
            }}
          >
            {phrase("Test")}
          </Button>
        </div>
        {result && (
          <p role="status" className="mt-2 text-sm">
            <StateBadge
              state={s(result.result).includes("skip") ? "pending" : "verified"}
              label={s(result.result).replace("_", " ")}
            />{" "}
            {s(result.detail)}
          </p>
        )}
      </div>
      <div>
        <p className="mb-2 text-sm font-medium">{phrase("Run history")}</p>
        <DataState
          q={runs}
          isEmpty={(d) => d.items.length === 0}
          empty={<p className="text-sm text-muted-foreground">{phrase("No runs yet.")}</p>}
        >
          {(d) => (
            <ul className="max-h-60 space-y-1 overflow-auto text-xs">
              {d.items.map((r) => (
                <li key={s(r.id)} className="flex flex-wrap gap-2">
                  <Time value={r.at} />
                  <StateBadge
                    state={
                      s(r.result) === "done"
                        ? "verified"
                        : s(r.result) === "failed"
                          ? "failed"
                          : "pending"
                    }
                    label={s(r.result) + (r.dry_run ? " (test)" : "")}
                  />
                  <span>{s(r.detail)}</span>
                  {r.inbound_id ? (
                    <Link className="text-primary" to={to(`inbox?m=${s(r.inbound_id)}`)}>
                      {s(r.subject) || phrase("message")}
                    </Link>
                  ) : null}
                  {r.message_id ? (
                    <Link className="text-primary" to={to(`messages/${s(r.message_id)}`)}>
                      {phrase("sent message")}
                    </Link>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </DataState>
      </div>
    </div>
  );
}

function RuleDialog({ rule, onClose }: { rule: Row | null; onClose: () => void }) {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const mailboxes = useQuery({
    queryKey: key("mailboxes"),
    queryFn: () => api.get<Page>("/mailboxes?limit=100"),
  });
  const templates = useQuery({
    queryKey: key("templates"),
    queryFn: () => api.get<Page>("/templates?limit=100"),
  });
  const webhooks = useQuery({
    queryKey: key("webhooks"),
    queryFn: () => api.get<Page>("/webhooks?limit=100"),
  });
  const [f, setF] = useState({
    name: s(rule?.name),
    mailbox_id: s(rule?.mailbox_id),
    cond_sender: s(rule?.cond_sender),
    cond_subject: s(rule?.cond_subject),
    cond_has_attachment: !!rule?.cond_has_attachment,
    action: s(rule?.action) || "auto_reply",
    target: s(rule?.target),
    template_id: s(rule?.template_id),
  });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f, v: string | boolean) => setF({ ...f, [k]: v });
  const targetError =
    f.action === "forward" && f.target && !emailOk(f.target)
      ? phrase("Enter an email address.")
      : f.action === "label" && f.target.length > 40
        ? phrase("At most 40 characters.")
        : "";
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogTitle>{rule ? phrase("Edit rule") : phrase("New rule")}</DialogTitle>
        <DialogDescription>
          {phrase(
            "New rules start switched off. Test one against a recent message before turning it on.",
          )}
        </DialogDescription>
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            if (targetError) return;
            setBusy(true);
            setError("");
            try {
              const body = { ...f, template_id: f.action === "auto_reply" ? f.template_id : "" };
              if (rule) await api.send(`/automations/${s(rule.id)}`, "PATCH", body);
              else await api.send("/automations", "POST", body);
              client.invalidateQueries({ queryKey: key("automations") });
              onClose();
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label={phrase("Name")} error={error}>
            <input
              className={inputClass}
              value={f.name}
              onChange={(e) => set("name", e.target.value)}
              required
            />
          </Field>
          <Field label={phrase("Mailbox")}>
            <select
              className={selectClass}
              value={f.mailbox_id}
              onChange={(e) => set("mailbox_id", e.target.value)}
            >
              <option value="">{phrase("All mailboxes")}</option>
              {mailboxes.data?.items.map((m) => (
                <option key={s(m.id)} value={s(m.id)}>
                  {s(m.address)}
                </option>
              ))}
            </select>
          </Field>
          <Section title={phrase("When (all that are filled must match)")} className="p-3">
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label={phrase("Sender contains")}>
                <input
                  className={inputClass}
                  value={f.cond_sender}
                  onChange={(e) => set("cond_sender", e.target.value)}
                  placeholder="@customer.example"
                />
              </Field>
              <Field label={phrase("Subject contains")}>
                <input
                  className={inputClass}
                  value={f.cond_subject}
                  onChange={(e) => set("cond_subject", e.target.value)}
                />
              </Field>
            </div>
            <label className="mt-2 flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={f.cond_has_attachment}
                onChange={(e) => set("cond_has_attachment", e.target.checked)}
              />{" "}
              {phrase("Has an attachment")}
            </label>
          </Section>
          <Field label={phrase("Then")}>
            <select
              className={selectClass}
              value={f.action}
              onChange={(e) => setF({ ...f, action: e.target.value, target: "" })}
            >
              {actions.map(([v, l]) => (
                <option key={v} value={v}>
                  {phrase(l)}
                </option>
              ))}
            </select>
          </Field>
          {f.action === "auto_reply" && (
            <Field
              label={phrase("Template")}
              hint={
                <>
                  {phrase(
                    "Published templates only. Variables available: subject, sender, mailbox.",
                  )}{" "}
                  <Link className="text-primary" to={to("templates")}>
                    {phrase("Templates")}
                  </Link>
                </>
              }
            >
              <select
                className={selectClass}
                value={f.template_id}
                onChange={(e) => set("template_id", e.target.value)}
              >
                <option value="">{phrase("Choose…")}</option>
                {templates.data?.items
                  .filter((t) => t.published_version)
                  .map((t) => (
                    <option key={s(t.id)} value={s(t.id)}>
                      {s(t.slug)}
                    </option>
                  ))}
              </select>
            </Field>
          )}
          {f.action === "forward" && (
            <Field label={phrase("Forward to")} error={targetError}>
              <input
                className={inputClass}
                type="email"
                value={f.target}
                onChange={(e) => set("target", e.target.value)}
              />
            </Field>
          )}
          {f.action === "label" && (
            <Field label={phrase("Label")} error={targetError}>
              <input
                className={inputClass}
                value={f.target}
                onChange={(e) => set("target", e.target.value)}
              />
            </Field>
          )}
          {f.action === "webhook" && (
            <Field
              label={phrase("Webhook endpoint")}
              hint={
                webhooks.data?.items.length ? undefined : (
                  <Link className="text-primary" to={to("webhooks")}>
                    {phrase("Add a webhook endpoint first")}
                  </Link>
                )
              }
            >
              <select
                className={selectClass}
                value={f.target}
                onChange={(e) => set("target", e.target.value)}
              >
                <option value="">{phrase("Choose…")}</option>
                {webhooks.data?.items.map((w) => (
                  <option key={s(w.id)} value={s(w.id)}>
                    {s(w.url)}
                  </option>
                ))}
              </select>
            </Field>
          )}
          <Button type="submit" disabled={busy || !f.name}>
            {busy ? phrase("Saving…") : phrase("Save rule")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
