import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { CopyButton } from "@/components/shared/CopyButton";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { useAuth } from "@/contexts/AuthContext";
import {
  controlClass,
  errText,
  Field,
  getJSON,
  Modal,
  PageTitle,
  send,
  ServerTable,
  show,
  StateBadge,
  useCan,
  useDraft,
  useList,
  When,
  type Row,
} from "@/components/admin/bits";

const EVENTS = ["email.submitted", "email.failed", "email.unknown", "email.expired", "email.bounced", "inbound.received"];

const SETTINGS = [
  ["inbound_retention_days", "Inbound retention days"],
  ["bounce_alert_min_volume", "Bounce alert minimum volume"],
  ["bounce_alert_hard_percent", "Hard-bounce alert percent"],
  ["bounce_alert_policy_percent", "Policy-bounce alert percent"],
  ["webhook_auto_disable_after", "Disable webhook after failures"],
  ["login_attempts_per_account", "Login attempts per account"],
  ["login_attempts_per_ip", "Login attempts per IP"],
  ["default_send_per_minute", "Default sends per minute"],
  ["default_send_per_day", "Default sends per day"],
] as const;

export function WebhooksPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({ endpoint_id: "", state: "" });
  const endpoints = useList("/webhooks", { offset: 0 });
  const deliveries = useList("/webhook-deliveries", { ...filters.applied, offset: filters.offset });
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [removeId, setRemoveId] = useState("");
  const [form, setForm] = useState({ project: "", mode: "live", url: "", events: [...EVENTS] });
  const create = useMutation({
    mutationFn: () => send("post", "/webhooks", form) as Promise<{ signing_secret: string }>,
    onSuccess: (data) => {
      setSecret(data.signing_secret);
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const rotate = useMutation({
    mutationFn: (id: string) => send("post", `/webhooks/${id}/rotate-secret`) as Promise<{ signing_secret: string }>,
    onSuccess: (data) => setSecret(data.signing_secret),
    onError: (error) => toast.error(errText(error)),
  });
  const replay = useMutation({
    mutationFn: (id: string) => send("post", `/webhook-deliveries/${id}/replay`),
    onSuccess: () => {
      toast.success("Replay queued.");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const remove = useMutation({
    mutationFn: () => send("delete", `/webhooks/${removeId}`),
    onSuccess: () => {
      setRemoveId("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-6">
      <PageTitle title="Webhooks" description="Payloads carry ids and states only. Receivers fetch inbound content from the API. Signatures use X-DM-Signature.">
        {canWrite && <Button onClick={() => setOpen(true)}>Add endpoint</Button>}
      </PageTitle>
      <ServerTable
        columns={[
          { header: "URL", cell: (row) => show(row.url) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Enabled", cell: (row) => row.enabled === true ? "yes" : `no ${show(row.disabled_reason)}` },
          { header: "Events", cell: (row) => show(row.events) },
          { header: "Backlog", cell: (row) => show(row.backlog) },
          {
            header: "",
            cell: (row) => canWrite ? (
              <div className="flex gap-2">
                <Button size="sm" variant="outline" onClick={() => rotate.mutate(show(row.id))}>Rotate secret</Button>
                <Button size="sm" variant="destructive" onClick={() => setRemoveId(show(row.id))}>Delete</Button>
              </div>
            ) : null,
          },
        ]}
        rows={endpoints.data?.items}
        loading={endpoints.isLoading}
        error={endpoints.error}
        empty="No webhook endpoints."
        offset={0}
        hasMore={false}
        onPage={() => undefined}
      />
      <form className="flex flex-wrap items-end gap-3" onSubmit={filters.apply}>
        <Field label="Endpoint id"><Input value={filters.draft.endpoint_id} onChange={(e) => filters.setDraft({ ...filters.draft, endpoint_id: e.target.value })} /></Field>
        <Field label="State">
          <select className={controlClass} value={filters.draft.state} onChange={(e) => filters.setDraft({ ...filters.draft, state: e.target.value })}>
            <option value="">Any</option>
            {["pending", "sending", "succeeded", "dead"].map((state) => <option key={state}>{state}</option>)}
          </select>
        </Field>
        <Button type="submit" variant="secondary">Filter deliveries</Button>
      </form>
      <ServerTable
        columns={[
          { header: "Event", cell: (row) => show(row.event_type) },
          { header: "State", cell: (row) => <StateBadge value={row.state} /> },
          { header: "Attempts", cell: (row) => show(row.attempts) },
          { header: "Response", cell: (row) => show(row.last_response_code) },
          { header: "Created", cell: (row) => <When value={row.created_at} /> },
          {
            header: "",
            cell: (row) => canWrite && (row.state === "succeeded" || row.state === "dead")
              ? <Button size="sm" variant="outline" onClick={() => replay.mutate(show(row.id))}>Replay</Button>
              : null,
          },
        ]}
        rows={deliveries.data?.items}
        loading={deliveries.isLoading}
        error={deliveries.error}
        empty="No deliveries."
        offset={filters.offset}
        hasMore={deliveries.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={open} title="Add webhook" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Create</Button>}>
        <div className="grid gap-3">
          <Field label="Project"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
          <Field label="Mode">
            <select className={controlClass} value={form.mode} onChange={(e) => setForm({ ...form, mode: e.target.value })}>
              <option value="sandbox">sandbox</option>
              <option value="live">live</option>
            </select>
          </Field>
          <Field label="HTTPS URL"><Input value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} /></Field>
          {EVENTS.map((event) => (
            <label key={event} className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.events.includes(event)} onChange={(e) => setForm({
                ...form,
                events: e.target.checked ? [...form.events, event] : form.events.filter((item) => item !== event),
              })} />
              {event}
            </label>
          ))}
        </div>
      </Modal>
      <Modal open={secret !== ""} title="Copy the signing secret" onClose={() => setSecret("")}>
        <p className="text-sm text-muted-foreground">It is shown once. Receivers verify X-DM-Signature with a five-minute tolerance.</p>
        <pre className="mt-3 overflow-x-auto rounded-md bg-muted p-3 text-xs">{secret}</pre>
        <div className="mt-3"><CopyButton value={secret} label="Copy secret" /></div>
      </Modal>
      <ConfirmDialog open={removeId !== ""} onOpenChange={(next) => !next && setRemoveId("")} title="Delete this endpoint?" description="Queued deliveries for it will stop." confirmLabel="Delete" variant="destructive" onConfirm={() => remove.mutate()} />
    </div>
  );
}

export function SuppressionsPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({ project: "", mode: "", source: "" });
  const query = useList("/suppressions", { ...filters.applied, offset: filters.offset });
  const [open, setOpen] = useState(false);
  const [removeId, setRemoveId] = useState("");
  const [reason, setReason] = useState("");
  const [form, setForm] = useState({ project: "", mode: "live", email: "", reason: "" });
  const create = useMutation({
    mutationFn: () => send("post", "/suppressions", form),
    onSuccess: () => {
      toast.success("Suppressed.");
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const remove = useMutation({
    mutationFn: () => send("delete", `/suppressions/${removeId}`, { reason }),
    onSuccess: () => {
      toast.success("Suppression removed.");
      setRemoveId("");
      setReason("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Suppressions" description="Addresses are masked. Hard bounces add rows automatically. Policy bounces do not.">
        {canWrite && <Button onClick={() => setOpen(true)}>Add</Button>}
      </PageTitle>
      <form className="flex flex-wrap items-end gap-3" onSubmit={filters.apply}>
        <Field label="Project"><Input value={filters.draft.project} onChange={(e) => filters.setDraft({ ...filters.draft, project: e.target.value })} /></Field>
        <Field label="Mode">
          <select className={controlClass} value={filters.draft.mode} onChange={(e) => filters.setDraft({ ...filters.draft, mode: e.target.value })}>
            <option value="">Any</option>
            <option value="sandbox">sandbox</option>
            <option value="live">live</option>
          </select>
        </Field>
        <Field label="Source">
          <select className={controlClass} value={filters.draft.source} onChange={(e) => filters.setDraft({ ...filters.draft, source: e.target.value })}>
            <option value="">Any</option>
            <option value="manual">manual</option>
            <option value="hard_bounce">hard_bounce</option>
          </select>
        </Field>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ServerTable
        columns={[
          { header: "Recipient", cell: (row) => show(row.recipient) },
          { header: "Source", cell: (row) => <StateBadge value={row.source} /> },
          { header: "Reason", cell: (row) => show(row.reason) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Created", cell: (row) => <When value={row.created_at} /> },
          { header: "", cell: (row) => canWrite ? <Button size="sm" variant="destructive" onClick={() => setRemoveId(show(row.id))}>Remove</Button> : null },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No suppressions."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={open} title="Suppress an address" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Add</Button>}>
        <div className="grid gap-3">
          <Field label="Project"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
          <Field label="Mode">
            <select className={controlClass} value={form.mode} onChange={(e) => setForm({ ...form, mode: e.target.value })}>
              <option value="sandbox">sandbox</option>
              <option value="live">live</option>
            </select>
          </Field>
          <Field label="Email"><Input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
          <Field label="Reason"><Input value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} /></Field>
        </div>
      </Modal>
      <Modal open={removeId !== ""} title="Remove suppression" onClose={() => setRemoveId("")} footer={<Button variant="destructive" onClick={() => remove.mutate()} disabled={reason.trim() === ""}>Remove</Button>}>
        <Field label="Reason"><Textarea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Why this address may receive mail again" /></Field>
      </Modal>
    </div>
  );
}

export function SandboxPage() {
  const filters = useDraft({});
  const query = useList("/sandbox", { offset: filters.offset });
  return (
    <div className="space-y-4">
      <PageTitle title="Sandbox" description="Captured messages never left this server. Admins and owners can open them." />
      <ServerTable
        columns={[
          { header: "Template", cell: (row) => `${show(row.template)} ${show(row.locale)}` },
          { header: "Project", cell: (row) => show(row.project) },
          { header: "Created", cell: (row) => <When value={row.created_at} /> },
          { header: "", cell: (row) => <Button variant="link" size="sm" asChild><Link to={`/sandbox/${show(row.id)}`}>Open</Link></Button> },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No sandbox captures."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
    </div>
  );
}

export function SandboxDetailPage() {
  const { id = "" } = useParams();
  const [html, setHtml] = useState(false);
  const query = useQuery({ queryKey: ["admin", "sandbox", id], queryFn: () => getJSON<Row>(`/sandbox/${id}`), enabled: Boolean(id) });
  return (
    <div className="space-y-4">
      <PageTitle title="Sandbox capture" description={show(query.data?.subject)} />
      {query.data && (
        <>
          <p className="text-sm">From {show(query.data.from)} · To {show(query.data.to)}</p>
          <div className="flex gap-2">
            <Button size="sm" variant={html ? "outline" : "default"} onClick={() => setHtml(false)}>Text</Button>
            <Button size="sm" variant={html ? "default" : "outline"} onClick={() => setHtml(true)}>HTML</Button>
          </div>
          {html ? (
            <iframe title="Sandbox HTML" sandbox="" className="h-[480px] w-full rounded-md border bg-white" srcDoc={typeof query.data.html === "string" ? query.data.html : ""} />
          ) : (
            <pre className="whitespace-pre-wrap rounded-xl border p-4 text-sm">{show(query.data.text)}</pre>
          )}
        </>
      )}
    </div>
  );
}

export function UsersPage() {
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const filters = useDraft({});
  const query = useList("/users", { offset: filters.offset });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ email: "", password: "", role: "admin" });
  const create = useMutation({
    mutationFn: () => send("post", "/users", form),
    onSuccess: () => {
      toast.success("User created.");
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const update = useMutation({
    mutationFn: ({ id, ...patch }: { id: string; role?: string; disabled?: boolean }) => send("patch", `/users/${id}`, patch),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["admin"] }),
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Users" description="Owners manage accounts. Passwords are at least 12 characters. There is no public signup.">
        <Button onClick={() => setOpen(true)}>Add user</Button>
      </PageTitle>
      <ServerTable
        columns={[
          { header: "Email", cell: (row) => show(row.email) },
          {
            header: "Role",
            cell: (row) => row.id === user?.id ? show(row.role) : (
              <select
                className={controlClass}
                value={show(row.role)}
                onChange={(event) => update.mutate({ id: show(row.id), role: event.target.value })}
              >
                <option value="admin">admin</option>
                <option value="owner">owner</option>
              </select>
            ),
          },
          { header: "Disabled", cell: (row) => show(row.disabled) },
          { header: "Last login", cell: (row) => <When value={row.last_login_at} /> },
          {
            header: "",
            cell: (row) => row.id === user?.id ? null : (
              <div className="flex gap-2">
                <Button size="sm" variant="outline" onClick={() => update.mutate({ id: show(row.id), disabled: row.disabled !== true })}>
                  {row.disabled === true ? "Enable" : "Disable"}
                </Button>
              </div>
            ),
          },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No users."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={open} title="Add user" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Create</Button>}>
        <div className="grid gap-3">
          <Field label="Email"><Input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
          <Field label="Password"><Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} /></Field>
          <Field label="Role">
            <select className={controlClass} value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}>
              <option value="admin">admin</option>
              <option value="owner">owner</option>
            </select>
          </Field>
        </div>
      </Modal>
    </div>
  );
}

export function AuditPage() {
  const filters = useDraft({ actor: "", action: "" });
  const query = useList("/audit", { ...filters.applied, offset: filters.offset });
  return (
    <div className="space-y-4">
      <PageTitle title="Audit log" description="Writes from the UI and the CLI. Details never include secrets or message bodies." />
      <form className="flex flex-wrap items-end gap-3" onSubmit={filters.apply}>
        <Field label="Actor"><Input value={filters.draft.actor} onChange={(e) => filters.setDraft({ ...filters.draft, actor: e.target.value })} /></Field>
        <Field label="Action"><Input value={filters.draft.action} onChange={(e) => filters.setDraft({ ...filters.draft, action: e.target.value })} /></Field>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ServerTable
        columns={[
          { header: "When", cell: (row) => <When value={row.at} /> },
          { header: "Actor", cell: (row) => show(row.actor) },
          { header: "Action", cell: (row) => show(row.action) },
          { header: "Target", cell: (row) => `${show(row.target_type)} ${show(row.target_id)}` },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No audit events."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
    </div>
  );
}

export function MailSettingsPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["admin", "settings"], queryFn: () => getJSON<Record<string, number>>("/settings") });
  const [draft, setDraft] = useState<Record<string, number> | null>(null);
  const values = draft ?? query.data ?? {};
  const save = useMutation({
    mutationFn: () => send("put", "/settings", values),
    onSuccess: () => {
      toast.success("Settings saved.");
      setDraft(null);
      void queryClient.invalidateQueries({ queryKey: ["admin", "settings"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Settings" description="Retention, rate-limit defaults and alert thresholds. Changes apply to new work." />
      <form className="grid max-w-lg gap-3" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}>
        {SETTINGS.map(([key, label]) => (
          <Field key={key} label={label}>
            <Input
              type="number"
              value={values[key] ?? ""}
              disabled={!canWrite}
              onChange={(e) => setDraft({ ...values, [key]: Number(e.target.value) })}
            />
          </Field>
        ))}
        {canWrite && <Button type="submit" disabled={save.isPending}>Save</Button>}
      </form>
    </div>
  );
}

export function AccountPage() {
  const { user } = useAuth();
  return (
    <div className="space-y-3">
      <PageTitle title="Account" description="This session is an operator account. Password changes are done by an owner or with the CLI." />
      <div className="max-w-md space-y-2 rounded-xl border p-4 text-sm">
        <p>{user?.email}</p>
        <p>Role <StateBadge value={user?.role} /></p>
        <p className="text-muted-foreground">Session expires <When value={user?.createdAt} /></p>
      </div>
    </div>
  );
}

export default WebhooksPage;
