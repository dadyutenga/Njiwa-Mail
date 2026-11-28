import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Code2, Plus, Trash2, Upload, UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useSendingEnvironment } from "../context";
import { SendingMode, useSendingModeParam, withSendingMode } from "../SendingMode";
import {
  ConfirmButton,
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
  textareaClass,
  Time,
  useOffset,
  useParam,
  type Page,
  type Row,
} from "../ui";

const statusState: Record<string, string> = {
  active: "active",
  unsubscribed: "cancelled",
  bounced: "failed",
  complained: "failed",
};

function useAudience() {
  const { to: workspaceTo } = useWorkspace();
  const [env, setEnv] = useSendingModeParam();
  const ctx = useSendingEnvironment(env);
  const to = (path: string) => withSendingMode(workspaceTo(path), env);
  return { env, setEnv, to, ...ctx };
}

// ---------------------------------------------------------------- contacts

export function Contacts() {
  const phrase = usePhrase();
  const { env, setEnv, api, key, to } = useAudience();
  const [status, setStatus] = useParam("status");
  const [list, setList] = useParam("list");
  const [q, setQ] = useParam("q");
  const [search, setSearch] = useState(q);
  const [importing, setImporting] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Row | null>(null);
  const [deleting, setDeleting] = useState<Row | null>(null);
  const offset = useOffset();
  const lists = useQuery({
    queryKey: key("lists"),
    queryFn: () => api.get<{ items: Row[] }>("/lists"),
  });
  const params = new URLSearchParams({ limit: "50", offset: String(offset) });
  if (status) params.set("status", status);
  if (list) params.set("list", list);
  if (q) params.set("q", q);
  const page = useQuery({
    queryKey: key("contacts", params.toString()),
    queryFn: () => api.get<Page>(`/contacts?${params}`),
  });
  return (
    <>
      <PageHeader
        title={phrase("Audience")}
        description={phrase(
          "The people you can mail, and the lists they belong to. A bounce, a complaint or an unsubscribe is shown here and always respected.",
        )}
        action={
          <span className="flex gap-2">
            <Button variant="outline" onClick={() => setImporting(true)}>
              <Upload size={16} /> {phrase("Import")}
            </Button>
            <Button onClick={() => setAdding(true)}>
              <Plus size={16} /> {phrase("Add contact")}
            </Button>
          </span>
        }
      />
      <section className="mb-4 rounded-xl border bg-card p-4">
        <div className="grid gap-3 lg:grid-cols-[minmax(12rem,auto)_9rem_10rem_1fr_auto] lg:items-end">
          <SendingMode value={env} onChange={setEnv} />
          <Field label={phrase("Status")}>
            <select
              className={selectClass}
              value={status}
              onChange={(e) => setStatus(e.target.value)}
            >
              <option value="">{phrase("All")}</option>
              {["active", "unsubscribed", "bounced", "complained"].map((x) => (
                <option key={x} value={x}>
                  {phrase(x)}
                </option>
              ))}
            </select>
          </Field>
          <Field label={phrase("List")}>
            <select className={selectClass} value={list} onChange={(e) => setList(e.target.value)}>
              <option value="">{phrase("All")}</option>
              {lists.data?.items.map((l) => (
                <option key={s(l.id)} value={s(l.id)}>
                  {s(l.name)}
                </option>
              ))}
            </select>
          </Field>
          <form
            className="flex items-end gap-2 lg:col-span-2"
            onSubmit={(e) => {
              e.preventDefault();
              setQ(search.trim());
            }}
          >
            <Field label={phrase("Find by exact address")} className="min-w-0 flex-1">
              <input
                className={inputClass}
                type="email"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="name@example.com"
              />
            </Field>
            <Button type="submit" variant="outline">
              {phrase("Find")}
            </Button>
          </form>
        </div>
      </section>
      <Lists />
      <DataState
        q={page}
        isEmpty={(d) => d.items.length === 0}
        empty={
          q || status || list ? (
            <div className="rounded-xl border bg-card px-6 py-12 text-center">
              <p className="font-semibold">{phrase("No matching contacts")}</p>
              <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
                {phrase("Nothing matches these filters. Clear them to see everyone.")}
              </p>
              <Button
                className="mt-4"
                variant="outline"
                onClick={() => {
                  setSearch("");
                  setQ("");
                  setStatus("");
                  setList("");
                }}
              >
                {phrase("Clear")}
              </Button>
            </div>
          ) : (
            <div className="rounded-xl border bg-card p-6">
              <h2 className="text-lg font-semibold">{phrase("No contacts")}</h2>
              <p className="mt-1 max-w-xl text-sm text-muted-foreground">
                {phrase(
                  "Import a CSV or JSON file, add someone, or let your application add sign-ups with the API.",
                )}
              </p>
              <div className="mt-5 grid gap-3 md:grid-cols-3">
                <button
                  type="button"
                  className="rounded-lg border bg-background p-4 text-left transition hover:border-primary hover:bg-primary/5"
                  onClick={() => setImporting(true)}
                >
                  <span className="flex size-9 items-center justify-center rounded-md bg-primary/10 text-primary">
                    <Upload size={18} aria-hidden />
                  </span>
                  <span className="mt-3 block font-medium">{phrase("Import a file")}</span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {phrase("CSV or JSON, with an email column.")}
                  </span>
                </button>
                <button
                  type="button"
                  className="rounded-lg border bg-background p-4 text-left transition hover:border-primary hover:bg-primary/5"
                  onClick={() => setAdding(true)}
                >
                  <span className="flex size-9 items-center justify-center rounded-md bg-primary/10 text-primary">
                    <UserPlus size={18} aria-hidden />
                  </span>
                  <span className="mt-3 block font-medium">{phrase("Add one person")}</span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {phrase("Type an address and put them on a list.")}
                  </span>
                </button>
                <div className="rounded-lg border bg-background p-4">
                  <span className="flex size-9 items-center justify-center rounded-md bg-muted text-muted-foreground">
                    <Code2 size={18} aria-hidden />
                  </span>
                  <span className="mt-3 block font-medium">{phrase("From your application")}</span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {phrase("Sign-ups can be added with the API.")}
                  </span>
                </div>
              </div>
            </div>
          )
        }
      >
        {(d) => (
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                {
                  key: "email_masked",
                  label: phrase("Address"),
                  render: (r) => (
                    <Link className="font-medium text-primary" to={to(`contacts/${s(r.id)}`)}>
                      {s(r.email_masked)}
                    </Link>
                  ),
                },
                {
                  key: "status",
                  label: phrase("Status"),
                  render: (r) => (
                    <StateBadge
                      state={statusState[s(r.status)] ?? "cancelled"}
                      label={phrase(s(r.status))}
                    />
                  ),
                },
                { key: "lists", label: phrase("Lists") },
                {
                  key: "created_at",
                  label: phrase("Added"),
                  render: (r) => <Time value={r.created_at} relative />,
                },
                {
                  key: "actions",
                  label: phrase("Actions"),
                  render: (r) => (
                    <span className="flex justify-end gap-2">
                      <Button size="sm" variant="outline" onClick={() => setEditing(r)}>
                        {phrase("Edit")}
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => setDeleting(r)}>
                        {phrase("Delete")}
                      </Button>
                    </span>
                  ),
                },
              ]}
            />
            <div className="px-4 pb-3">
              <Pager page={d} />
            </div>
          </div>
        )}
      </DataState>
      <ImportDialog open={importing} onOpenChange={setImporting} lists={lists.data?.items ?? []} />
      <ContactDialog open={adding} onOpenChange={setAdding} lists={lists.data?.items ?? []} />
      <EditContactDialog
        contact={editing}
        lists={lists.data?.items ?? []}
        onClose={() => setEditing(null)}
      />
      <DeleteContactDialog contact={deleting} onClose={() => setDeleting(null)} />
    </>
  );
}

function ContactDialog({
  open,
  onOpenChange,
  lists,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  lists: Row[];
}) {
  const phrase = usePhrase();
  const { api, key } = useAudience();
  const client = useQueryClient();
  const [f, setF] = useState({ email: "", name: "", list: "" });
  const [error, setError] = useState("");
  const save = async () => {
    setError("");
    try {
      await api.send("/contacts", "POST", {
        email: f.email.trim(),
        name: f.name.trim(),
        lists: f.list ? [f.list] : [],
      });
      client.invalidateQueries({ queryKey: key("contacts") });
      client.invalidateQueries({ queryKey: key("lists") });
      setF({ email: "", name: "", list: "" });
      onOpenChange(false);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("Add contact")}</DialogTitle>
        <DialogDescription>
          {phrase("Add only people who agreed to receive your marketing mail.")}
        </DialogDescription>
        <div className="grid gap-3">
          <Field label={phrase("Email address")}>
            <input
              className={inputClass}
              type="email"
              value={f.email}
              onChange={(e) => setF({ ...f, email: e.target.value })}
            />
          </Field>
          <Field label={phrase("Name")}>
            <input
              className={inputClass}
              value={f.name}
              onChange={(e) => setF({ ...f, name: e.target.value })}
            />
          </Field>
          <Field label={phrase("Add to list")}>
            <select
              className={selectClass}
              value={f.list}
              onChange={(e) => setF({ ...f, list: e.target.value })}
            >
              <option value="">{phrase("None")}</option>
              {lists.map((l) => (
                <option key={s(l.id)} value={s(l.id)}>
                  {s(l.name)}
                </option>
              ))}
            </select>
          </Field>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button onClick={save} disabled={!f.email.trim()}>
            {phrase("Save")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

type ImportReport = {
  rows: number;
  created: number;
  updated: number;
  existing: number;
  invalid: number;
  errors: { row: number; reason: string }[];
  columns: string[];
  sample?: Row;
};

function ImportDialog({
  open,
  onOpenChange,
  lists,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  lists: Row[];
}) {
  const phrase = usePhrase();
  const { api, key } = useAudience();
  const client = useQueryClient();
  const [data, setData] = useState("");
  const [format, setFormat] = useState<"csv" | "json">("csv");
  const [emailCol, setEmailCol] = useState("");
  const [nameCol, setNameCol] = useState("");
  const [list, setList] = useState("");
  const [update, setUpdate] = useState(false);
  const [preview, setPreview] = useState<ImportReport | null>(null);
  const [done, setDone] = useState<ImportReport | null>(null);
  const [error, setError] = useState("");
  const body = () => ({
    format,
    data,
    list_id: list,
    update_existing: update,
    mapping: { email: emailCol, name: nameCol },
  });
  const check = async () => {
    setError("");
    setDone(null);
    try {
      setPreview(await api.send<ImportReport>("/contacts/import?dry_run=1", "POST", body()));
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const go = async () => {
    setError("");
    try {
      setDone(await api.send<ImportReport>("/contacts/import", "POST", body()));
      client.invalidateQueries({ queryKey: key("contacts") });
      client.invalidateQueries({ queryKey: key("lists") });
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const reset = (v: boolean) => {
    if (!v) {
      setData("");
      setPreview(null);
      setDone(null);
      setError("");
    }
    onOpenChange(v);
  };
  return (
    <Dialog open={open} onOpenChange={reset}>
      <DialogContent className="max-w-2xl">
        <DialogTitle>{phrase("Import contacts")}</DialogTitle>
        <DialogDescription>
          {phrase(
            "A CSV with a header row, or a JSON array of objects. Check first: nothing is saved until you import.",
          )}
        </DialogDescription>
        <div className="grid gap-3">
          <div className="flex flex-wrap items-end gap-2">
            <Field label={phrase("Format")}>
              <select
                className={selectClass}
                value={format}
                onChange={(e) => setFormat(e.target.value as "csv" | "json")}
              >
                <option value="csv">CSV</option>
                <option value="json">JSON</option>
              </select>
            </Field>
            <label className="inline-flex cursor-pointer items-center gap-1 rounded-md border px-3 py-2 text-sm hover:bg-muted">
              <Upload size={14} /> {phrase("Choose a file")}
              <input
                type="file"
                accept=".csv,.json,text/csv,application/json,text/plain"
                className="sr-only"
                onChange={async (e) => {
                  const file = e.target.files?.[0];
                  if (file) {
                    setData(await file.text());
                    if (file.name.endsWith(".json")) setFormat("json");
                    setPreview(null);
                  }
                }}
              />
            </label>
          </div>
          <textarea
            className={textareaClass + " font-mono text-xs"}
            rows={6}
            value={data}
            onChange={(e) => {
              setData(e.target.value);
              setPreview(null);
            }}
            placeholder={"email,name,plan\nasha@example.org,Asha,pro"}
          />
          {preview && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={phrase("Email column")}>
                <select
                  className={selectClass}
                  value={emailCol}
                  onChange={(e) => setEmailCol(e.target.value)}
                >
                  <option value="">{phrase("Detect")}</option>
                  {preview.columns.map((c) => (
                    <option key={c}>{c}</option>
                  ))}
                </select>
              </Field>
              <Field label={phrase("Name column")}>
                <select
                  className={selectClass}
                  value={nameCol}
                  onChange={(e) => setNameCol(e.target.value)}
                >
                  <option value="">{phrase("Detect")}</option>
                  {preview.columns.map((c) => (
                    <option key={c}>{c}</option>
                  ))}
                </select>
              </Field>
            </div>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={phrase("Add everyone to list")}>
              <select
                className={selectClass}
                value={list}
                onChange={(e) => setList(e.target.value)}
              >
                <option value="">{phrase("None")}</option>
                {lists.map((l) => (
                  <option key={s(l.id)} value={s(l.id)}>
                    {s(l.name)}
                  </option>
                ))}
              </select>
            </Field>
            <label className="flex items-center gap-2 self-end pb-2 text-sm">
              <input
                type="checkbox"
                checked={update}
                onChange={(e) => setUpdate(e.target.checked)}
              />
              {phrase("Update contacts that already exist")}
            </label>
          </div>
          {(preview || done) && (
            <ImportSummary r={(done ?? preview) as ImportReport} done={Boolean(done)} />
          )}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={check} disabled={!data.trim()}>
              {phrase("Check")}
            </Button>
            <Button onClick={go} disabled={!preview || Boolean(done)}>
              {phrase("Import")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function EditContactDialog({
  contact,
  lists,
  onClose,
}: {
  contact: Row | null;
  lists: Row[];
  onClose: () => void;
}) {
  const phrase = usePhrase();
  const { api, key } = useAudience();
  const client = useQueryClient();
  const id = contact ? s(contact.id) : "";
  const q = useQuery({
    queryKey: key("contact", id),
    queryFn: () => api.get<Row>(`/contacts/${id}`),
    enabled: !!id,
  });
  const [f, setF] = useState({ email: "", name: "", list: "" });
  const [error, setError] = useState("");
  useEffect(() => {
    if (!q.data) return;
    setF({ email: s(q.data.email), name: s(q.data.name), list: "" });
    setError("");
  }, [q.data]);
  const save = async () => {
    setError("");
    try {
      await api.send(`/contacts/${id}`, "PATCH", {
        email: f.email.trim(),
        name: f.name.trim(),
        lists: f.list ? [f.list] : [],
      });
      client.invalidateQueries({ queryKey: key("contacts") });
      client.invalidateQueries({ queryKey: key("contact", id) });
      client.invalidateQueries({ queryKey: key("lists") });
      onClose();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Dialog open={!!contact} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogTitle>{phrase("Edit contact")}</DialogTitle>
        <DialogDescription>
          {phrase(
            "Change the name or address. Choosing a list adds them to it and leaves their other lists as they are.",
          )}
        </DialogDescription>
        {q.isFetching && !q.data ? (
          <p role="status">{phrase("Loading…")}</p>
        ) : q.error && !q.data ? (
          <p role="alert">{q.error.message}</p>
        ) : (
          <div className="grid gap-3">
            <Field label={phrase("Email address")}>
              <input
                className={inputClass}
                type="email"
                value={f.email}
                onChange={(e) => setF({ ...f, email: e.target.value })}
              />
            </Field>
            <Field label={phrase("Name")}>
              <input
                className={inputClass}
                value={f.name}
                onChange={(e) => setF({ ...f, name: e.target.value })}
              />
            </Field>
            <Field label={phrase("Add to list")}>
              <select
                className={selectClass}
                value={f.list}
                onChange={(e) => setF({ ...f, list: e.target.value })}
              >
                <option value="">{phrase("None")}</option>
                {lists.map((l) => (
                  <option key={s(l.id)} value={s(l.id)}>
                    {s(l.name)}
                  </option>
                ))}
              </select>
            </Field>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <div className="flex gap-2">
              <Button onClick={save} disabled={!f.email.trim()}>
                {phrase("Save")}
              </Button>
              <Button type="button" variant="outline" onClick={onClose}>
                {phrase("Cancel")}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function DeleteContactDialog({ contact, onClose }: { contact: Row | null; onClose: () => void }) {
  const phrase = usePhrase();
  const { api, key } = useAudience();
  const client = useQueryClient();
  const [error, setError] = useState("");
  const remove = async () => {
    if (!contact) return;
    setError("");
    try {
      await api.send(`/contacts/${s(contact.id)}`, "DELETE");
      client.invalidateQueries({ queryKey: key("contacts") });
      onClose();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Dialog open={!!contact} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogTitle>{phrase("Delete contact")}</DialogTitle>
        <DialogDescription>
          {s(contact?.email_masked)}
          {". "}
          {phrase("Their list memberships go too. Suppressions and unsubscribes stay.")}
        </DialogDescription>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="flex gap-2">
          <Button variant="destructive" onClick={remove}>
            {phrase("Delete")}
          </Button>
          <Button type="button" variant="outline" onClick={onClose}>
            {phrase("Cancel")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ImportSummary({ r, done }: { r: ImportReport; done: boolean }) {
  const phrase = usePhrase();
  return (
    <div role="status" className="rounded-lg border bg-muted/40 p-3 text-sm">
      <p className="font-medium">
        {r.rows} {phrase("rows")}
        {done
          ? ` · ${r.created} ${phrase("added")} · ${r.updated} ${phrase("updated")} · ${r.existing} ${phrase("already there")}`
          : ""}
        {` · ${r.invalid} ${phrase("not imported")}`}
      </p>
      {!done && r.sample && (
        <p className="mt-1 text-xs text-muted-foreground">
          {phrase("First row:")} {s(r.sample.email)} {s(r.sample.name)}{" "}
          {JSON.stringify(r.sample.attributes ?? {})}
        </p>
      )}
      {r.errors.length > 0 && (
        <ul className="mt-2 max-h-28 overflow-auto text-xs text-muted-foreground">
          {r.errors.map((e) => (
            <li key={`${e.row}-${e.reason}`}>
              {phrase("Row")} {e.row}: {e.reason.replace(/_/g, " ")}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function ContactDetail() {
  const phrase = usePhrase();
  const { id = "" } = useParams();
  const { api, key, to } = useAudience();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: key("contact", id),
    queryFn: () => api.get<Row>(`/contacts/${id}`),
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: key("contact", id) });
    client.invalidateQueries({ queryKey: key("contacts") });
  };
  return (
    <DataState q={q}>
      {(c) => (
        <>
          <PageHeader
            back={{ to: to("contacts"), label: phrase("Contacts") }}
            title={s(c.email)}
            description={
              <>
                <StateBadge
                  state={statusState[s(c.status)] ?? "cancelled"}
                  label={phrase(s(c.status))}
                />{" "}
                {s(c.name)}
              </>
            }
            action={
              <ConfirmButton
                title={phrase("Delete this contact?")}
                description={phrase(
                  "Their list memberships go too. Suppressions and unsubscribes stay.",
                )}
                confirm="Delete"
                onConfirm={async () => {
                  await run(
                    () => api.send(`/contacts/${id}`, "DELETE"),
                    phrase("Contact deleted."),
                  );
                  client.invalidateQueries({ queryKey: key("contacts") });
                  history.back();
                }}
              >
                <Trash2 size={14} /> {phrase("Delete")}
              </ConfirmButton>
            }
          />
          <div className="grid gap-4 lg:grid-cols-2">
            <Section title={phrase("Messages to this address")}>
              <dl className="grid grid-cols-2 gap-2 text-sm">
                {(
                  [
                    ["All messages", c.messages],
                    ["Submitted", c.submitted],
                    ["Failed", c.failed],
                    ["Bounced", c.bounced],
                  ] as const
                ).map(([label, v]) => (
                  <div key={label} className="rounded-lg border p-2">
                    <dt className="text-xs text-muted-foreground">{phrase(label)}</dt>
                    <dd className="text-lg font-semibold">{s(v) || "0"}</dd>
                  </div>
                ))}
              </dl>
              <p className="mt-2 text-xs text-muted-foreground">
                {phrase("Last message:")} <Time value={c.last_message_at} relative />
              </p>
              {s(c.status) === "unsubscribed" && (
                <Button
                  className="mt-3"
                  variant="outline"
                  size="sm"
                  onClick={async () => {
                    await run(
                      () => api.send(`/contacts/${id}/resubscribe`, "POST", {}),
                      phrase("Resubscribed."),
                    );
                    refresh();
                  }}
                >
                  {phrase("Resubscribe to all marketing mail")}
                </Button>
              )}
            </Section>
            <Section title={phrase("Lists")}>
              {((c.list_memberships as Row[]) ?? []).length === 0 ? (
                <p className="text-sm text-muted-foreground">{phrase("Not on any list.")}</p>
              ) : (
                <ul className="divide-y text-sm">
                  {(c.list_memberships as Row[]).map((m) => (
                    <li key={s(m.id)} className="flex items-center justify-between gap-2 py-2">
                      <span>
                        {s(m.name)}
                        {m.unsubscribed_at ? (
                          <span className="ml-2 text-xs text-muted-foreground">
                            ({phrase("unsubscribed")})
                          </span>
                        ) : null}
                      </span>
                      {m.unsubscribed_at ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={async () => {
                            await run(
                              () =>
                                api.send(`/contacts/${id}/resubscribe`, "POST", {
                                  list_id: s(m.id),
                                }),
                              phrase("Resubscribed."),
                            );
                            refresh();
                          }}
                        >
                          {phrase("Resubscribe")}
                        </Button>
                      ) : null}
                    </li>
                  ))}
                </ul>
              )}
            </Section>
            <Section title={phrase("Campaigns and newsletters")} className="lg:col-span-2">
              {((c.campaigns as Row[]) ?? []).length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {phrase("Not included in any yet.")}
                </p>
              ) : (
                <ul className="divide-y text-sm">
                  {(c.campaigns as Row[]).map((x) => (
                    <li key={s(x.id)} className="flex flex-wrap items-center gap-2 py-2">
                      <Link
                        className="mr-auto font-medium text-primary"
                        to={to(
                          x.newsletter_id ? `newsletters/${s(x.newsletter_id)}` : `bulk/${s(x.id)}`,
                        )}
                      >
                        {s(x.name)}
                      </Link>
                      <StateBadge state={s(x.state)} />
                      {x.skip_reason ? (
                        <span className="text-xs text-muted-foreground">{s(x.skip_reason)}</span>
                      ) : null}
                      <Time value={x.created_at} />
                    </li>
                  ))}
                </ul>
              )}
            </Section>
            <Section title={phrase("Attributes")} className="lg:col-span-2">
              <pre className="whitespace-pre-wrap text-xs">
                {JSON.stringify(c.attributes ?? {}, null, 2)}
              </pre>
              <p className="mt-2 text-xs text-muted-foreground">
                {phrase(
                  "Attributes are extra details, such as a plan or a city. Do not store secrets in them.",
                )}
              </p>
            </Section>
          </div>
        </>
      )}
    </DataState>
  );
}

// ---------------------------------------------------------------- lists

export function Lists() {
  const phrase = usePhrase();
  const { api, key, to } = useAudience();
  const client = useQueryClient();
  const [name, setName] = useState("");
  const q = useQuery({
    queryKey: key("lists"),
    queryFn: () => api.get<{ items: Row[] }>("/lists"),
  });
  const create = async () => {
    const r = await run(
      () => api.send("/lists", "POST", { name: name.trim() }),
      phrase("List created."),
    );
    if (r) setName("");
    client.invalidateQueries({ queryKey: key("lists") });
  };
  return (
    <Section className="mb-4" title={phrase("Lists")}>
      <p className="mb-3 text-sm text-muted-foreground">
        {phrase("A named group of contacts. Unsubscribing from a list leaves that list only.")}
      </p>
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) create();
          }}
        >
          <Field label={phrase("New list")}>
            <input
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={phrase("Newsletter")}
            />
          </Field>
          <Button type="submit" disabled={!name.trim()}>
            <Plus size={16} /> {phrase("Create")}
          </Button>
        </form>
      </div>
      <DataState
        q={q}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <Empty
            title={phrase("No lists")}
            description={phrase("Create a list, then import contacts into it.")}
          />
        }
      >
        {(d) => (
          <Table
            rows={d.items}
            rowKey={(r) => s(r.id)}
            columns={[
              {
                key: "name",
                label: phrase("Name"),
                render: (r) => (
                  <Link className="font-medium text-primary" to={to(`contacts?list=${s(r.id)}`)}>
                    {s(r.name)}
                  </Link>
                ),
              },
              { key: "members", label: phrase("Members") },
              { key: "unsubscribed", label: phrase("Unsubscribed") },
              {
                key: "created_at",
                label: phrase("Created"),
                render: (r) => <Time value={r.created_at} relative />,
              },
              {
                key: "send",
                label: <span className="sr-only">{phrase("Send")}</span>,
                render: (r) => (
                  <Button asChild size="sm" variant="outline">
                    <Link to={to(`bulk/new?list=${s(r.id)}`)}>
                      {phrase("Send a campaign to this list")}
                    </Link>
                  </Button>
                ),
              },
              {
                key: "x",
                label: <span className="sr-only">{phrase("Actions")}</span>,
                render: (r) => (
                  <ConfirmButton
                    title={`${phrase("Delete")} ${s(r.name)}?`}
                    description={phrase(
                      "The contacts stay; only the list and its memberships are deleted.",
                    )}
                    confirm="Delete list"
                    onConfirm={async () => {
                      await run(
                        () => api.send(`/lists/${s(r.id)}`, "DELETE"),
                        phrase("List deleted."),
                      );
                      client.invalidateQueries({ queryKey: key("lists") });
                    }}
                  >
                    <Trash2 size={14} />
                  </ConfirmButton>
                ),
              },
            ]}
          />
        )}
      </DataState>
    </Section>
  );
}
