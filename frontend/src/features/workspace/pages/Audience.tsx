import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, Upload } from "lucide-react";
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
        title={phrase("Contacts")}
        description={phrase(
          "People who receive marketing mail. A bounce, a complaint or an unsubscribe is shown here and always respected.",
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
      <div className="mb-4 flex flex-wrap items-end gap-3">
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
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setQ(search.trim());
          }}
        >
          <Field label={phrase("Find by exact address")}>
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
      <DataState
        q={page}
        isEmpty={(d) => d.items.length === 0}
        empty={
          <div className="rounded-xl border bg-card">
            <Empty
              title={phrase("No contacts")}
              description={phrase(
                "Import a CSV or JSON file, add someone, or let your application add sign-ups with the API.",
              )}
              action={
                <Button onClick={() => setImporting(true)}>{phrase("Import contacts")}</Button>
              }
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
              ]}
            />
            <Pager page={d} />
          </>
        )}
      </DataState>
      <ImportDialog open={importing} onOpenChange={setImporting} lists={lists.data?.items ?? []} />
      <ContactDialog open={adding} onOpenChange={setAdding} lists={lists.data?.items ?? []} />
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
            <Section title={phrase("Attributes")} className="lg:col-span-2">
              <pre className="whitespace-pre-wrap text-xs">
                {JSON.stringify(c.attributes ?? {}, null, 2)}
              </pre>
              <p className="mt-2 text-xs text-muted-foreground">
                {phrase(
                  "Attributes are for segments, such as a plan or a city. Do not store secrets in them.",
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
  const { env, setEnv, api, key, to } = useAudience();
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
    <>
      <PageHeader
        title={phrase("Lists")}
        description={phrase(
          "Subscriber lists. Unsubscribing from a list's mail leaves that list only, unless the reader chooses all marketing mail.",
        )}
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <SendingMode value={env} onChange={setEnv} />
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
    </>
  );
}

// ---------------------------------------------------------------- segments

type Condition = { field: string; op: string; key?: string; value?: string };

const fieldOps: Record<string, string[]> = {
  status: ["eq", "neq"],
  list: ["in", "not_in"],
  domain: ["eq", "neq"],
  attribute: ["eq", "neq", "contains", "exists", "not_exists"],
  created: ["after", "before"],
};

export function Segments() {
  const phrase = usePhrase();
  const { env, setEnv, api, key } = useAudience();
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [match, setMatch] = useState<"all" | "any">("all");
  const [conds, setConds] = useState<Condition[]>([{ field: "status", op: "eq", value: "active" }]);
  const [preview, setPreview] = useState<Row | null>(null);
  const [error, setError] = useState("");
  const lists = useQuery({
    queryKey: key("lists"),
    queryFn: () => api.get<{ items: Row[] }>("/lists"),
  });
  const q = useQuery({
    queryKey: key("segments"),
    queryFn: () => api.get<{ items: Row[] }>("/segments"),
  });
  const rules = () => ({
    match,
    conditions: conds.map((c) =>
      c.field === "created" && c.value ? { ...c, value: new Date(c.value).toISOString() } : c,
    ),
  });
  const setCond = (i: number, c: Partial<Condition>) => {
    setConds(conds.map((x, j) => (j === i ? { ...x, ...c } : x)));
    setPreview(null);
  };
  const check = async () => {
    setError("");
    try {
      setPreview(await api.send<Row>("/segments/preview", "POST", rules()));
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const save = async () => {
    setError("");
    try {
      await api.send("/segments", "POST", { name: name.trim(), rules: rules() });
      setName("");
      client.invalidateQueries({ queryKey: key("segments") });
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <>
      <PageHeader
        title={phrase("Segments")}
        description={phrase(
          "Audiences from rules, worked out again each time a campaign is created.",
        )}
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} />
      </div>
      <Section title={phrase("New segment")}>
        <div className="grid gap-3">
          <Field label={phrase("Name")}>
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={phrase("Match")}>
            <select
              className={selectClass}
              value={match}
              onChange={(e) => setMatch(e.target.value as "all" | "any")}
            >
              <option value="all">{phrase("All conditions")}</option>
              <option value="any">{phrase("Any condition")}</option>
            </select>
          </Field>
          {conds.map((c, i) => (
            <div key={i} className="flex flex-wrap items-end gap-2">
              <select
                aria-label={phrase("Field")}
                className={selectClass + " w-36"}
                value={c.field}
                onChange={(e) =>
                  setCond(i, {
                    field: e.target.value,
                    op: fieldOps[e.target.value][0],
                    value: "",
                    key: "",
                  })
                }
              >
                {Object.keys(fieldOps).map((f) => (
                  <option key={f} value={f}>
                    {phrase(f)}
                  </option>
                ))}
              </select>
              {c.field === "attribute" && (
                <input
                  aria-label={phrase("Attribute")}
                  className={inputClass + " w-32"}
                  value={c.key ?? ""}
                  onChange={(e) => setCond(i, { key: e.target.value })}
                  placeholder="plan"
                />
              )}
              <select
                aria-label={phrase("Operator")}
                className={selectClass + " w-36"}
                value={c.op}
                onChange={(e) => setCond(i, { op: e.target.value })}
              >
                {fieldOps[c.field].map((o) => (
                  <option key={o} value={o}>
                    {phrase(o.replace(/_/g, " "))}
                  </option>
                ))}
              </select>
              {c.field === "status" ? (
                <select
                  aria-label={phrase("Value")}
                  className={selectClass + " w-40"}
                  value={c.value}
                  onChange={(e) => setCond(i, { value: e.target.value })}
                >
                  {["active", "unsubscribed", "bounced", "complained"].map((x) => (
                    <option key={x} value={x}>
                      {phrase(x)}
                    </option>
                  ))}
                </select>
              ) : c.field === "list" ? (
                <select
                  aria-label={phrase("Value")}
                  className={selectClass + " w-48"}
                  value={c.value}
                  onChange={(e) => setCond(i, { value: e.target.value })}
                >
                  <option value="">{phrase("Choose…")}</option>
                  {lists.data?.items.map((l) => (
                    <option key={s(l.id)} value={s(l.id)}>
                      {s(l.name)}
                    </option>
                  ))}
                </select>
              ) : c.op === "exists" || c.op === "not_exists" ? null : (
                <input
                  aria-label={phrase("Value")}
                  type={c.field === "created" ? "datetime-local" : "text"}
                  className={inputClass + " w-48"}
                  value={c.value ?? ""}
                  onChange={(e) => setCond(i, { value: e.target.value })}
                />
              )}
              {conds.length > 1 && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setConds(conds.filter((_, j) => j !== i))}
                >
                  <Trash2 size={14} />
                  <span className="sr-only">{phrase("Remove condition")}</span>
                </Button>
              )}
            </div>
          ))}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={conds.length >= 20}
              onClick={() =>
                setConds([...conds, { field: "attribute", op: "eq", key: "", value: "" }])
              }
            >
              <Plus size={14} /> {phrase("Add condition")}
            </Button>
            <Button variant="outline" size="sm" onClick={check}>
              {phrase("Preview")}
            </Button>
            <Button size="sm" onClick={save} disabled={!name.trim()}>
              {phrase("Save segment")}
            </Button>
          </div>
          {preview && (
            <p role="status" className="text-sm">
              {s(preview.matches)} {phrase("contacts match")} · {s(preview.sendable)}{" "}
              {phrase("can receive marketing mail now")}
            </p>
          )}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
        </div>
      </Section>
      <DataState q={q} isEmpty={(d) => d.items.length === 0} empty={null}>
        {(d) => (
          <Section className="mt-4" title={phrase("Saved segments")}>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.id)}
              columns={[
                { key: "name", label: phrase("Name") },
                {
                  key: "created_at",
                  label: phrase("Created"),
                  render: (r) => <Time value={r.created_at} relative />,
                },
                {
                  key: "x",
                  label: <span className="sr-only">{phrase("Actions")}</span>,
                  render: (r) => (
                    <ConfirmButton
                      title={`${phrase("Delete")} ${s(r.name)}?`}
                      description={phrase("Campaigns already created keep their recipients.")}
                      confirm="Delete segment"
                      onConfirm={async () => {
                        await run(
                          () => api.send(`/segments/${s(r.id)}`, "DELETE"),
                          phrase("Segment deleted."),
                        );
                        client.invalidateQueries({ queryKey: key("segments") });
                      }}
                    >
                      <Trash2 size={14} />
                    </ConfirmButton>
                  ),
                },
              ]}
            />
          </Section>
        )}
      </DataState>
    </>
  );
}
