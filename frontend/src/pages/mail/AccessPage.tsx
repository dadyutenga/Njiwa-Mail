import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { CopyButton } from "@/components/shared/CopyButton";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import {
  controlClass,
  errText,
  Field,
  getJSON,
  lines,
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

const SCOPES = ["emails:send", "emails:read", "inbound:read", "suppressions:manage"];

export function ProjectsPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({});
  const query = useList("/projects", { offset: filters.offset });
  const [name, setName] = useState("");
  const [edit, setEdit] = useState<Row | null>(null);
  const create = useMutation({
    mutationFn: () => send("post", "/projects", { name }),
    onSuccess: () => {
      toast.success("Project created.");
      setName("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const save = useMutation({
    mutationFn: async () => {
      if (!edit) return;
      await send("patch", `/projects/${show(edit.name)}`, {
        enabled: edit.enabled === true,
        send_per_minute: Number(edit.send_per_minute),
        send_per_day: Number(edit.send_per_day),
        recipient_per_minute: Number(edit.recipient_per_minute),
        recipient_per_hour: Number(edit.recipient_per_hour),
      });
      const environments = (edit.environments as Row[]) ?? [];
      for (const environment of environments) {
        await send("patch", `/projects/${show(edit.name)}/environments/${show(environment.mode)}`, {
          enabled: environment.enabled === true,
          restrict_recipients: environment.restrict_recipients === true,
          approved_origins: environment.approved_origins,
          recipient_allowlist: environment.recipient_allowlist,
        });
      }
    },
    onSuccess: () => {
      toast.success("Project saved.");
      setEdit(null);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Projects" description="Each project has a sandbox and a live environment. Limits and approved URL origins are enforced on every send." />
      {canWrite && (
        <form className="flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}>
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="sacas" /></Field>
          <Button type="submit" disabled={create.isPending || name.trim() === ""}>Create</Button>
        </form>
      )}
      <ServerTable
        columns={[
          { header: "Name", cell: (row) => show(row.name) },
          { header: "Enabled", cell: (row) => show(row.enabled) },
          { header: "Per minute", cell: (row) => show(row.send_per_minute) },
          { header: "Per day", cell: (row) => show(row.send_per_day) },
          { header: "Created", cell: (row) => <When value={row.created_at} /> },
          { header: "", cell: (row) => canWrite ? <Button size="sm" variant="outline" onClick={() => setEdit(structuredClone(row))}>Edit</Button> : null },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No projects yet."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={edit !== null} title={edit ? `Edit ${show(edit.name)}` : "Project"} onClose={() => setEdit(null)} footer={<Button onClick={() => save.mutate()} disabled={save.isPending}>Save</Button>}>
        {edit && (
          <div className="grid gap-3">
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={edit.enabled === true} onChange={(e) => setEdit({ ...edit, enabled: e.target.checked })} /> Enabled</label>
            <Field label="Send per minute"><Input type="number" value={show(edit.send_per_minute)} onChange={(e) => setEdit({ ...edit, send_per_minute: Number(e.target.value) })} /></Field>
            <Field label="Send per day"><Input type="number" value={show(edit.send_per_day)} onChange={(e) => setEdit({ ...edit, send_per_day: Number(e.target.value) })} /></Field>
            <Field label="Recipient per minute"><Input type="number" value={show(edit.recipient_per_minute)} onChange={(e) => setEdit({ ...edit, recipient_per_minute: Number(e.target.value) })} /></Field>
            <Field label="Recipient per hour"><Input type="number" value={show(edit.recipient_per_hour)} onChange={(e) => setEdit({ ...edit, recipient_per_hour: Number(e.target.value) })} /></Field>
            {((edit.environments as Row[]) ?? []).map((environment, index) => (
              <div key={show(environment.mode)} className="grid gap-2 rounded-lg border p-3">
                <p className="font-medium">{show(environment.mode)}</p>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={environment.enabled === true} onChange={(e) => {
                  const environments = [...((edit.environments as Row[]) ?? [])];
                  environments[index] = { ...environment, enabled: e.target.checked };
                  setEdit({ ...edit, environments });
                }} /> Enabled</label>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={environment.restrict_recipients === true} onChange={(e) => {
                  const environments = [...((edit.environments as Row[]) ?? [])];
                  environments[index] = { ...environment, restrict_recipients: e.target.checked };
                  setEdit({ ...edit, environments });
                }} /> Restrict recipients</label>
                <Field label="Approved origins">
                  <Textarea value={((environment.approved_origins as string[]) ?? []).join("\n")} onChange={(e) => {
                    const environments = [...((edit.environments as Row[]) ?? [])];
                    environments[index] = { ...environment, approved_origins: lines(e.target.value) };
                    setEdit({ ...edit, environments });
                  }} />
                </Field>
                <Field label="Recipient allowlist">
                  <Textarea value={((environment.recipient_allowlist as string[]) ?? []).join("\n")} onChange={(e) => {
                    const environments = [...((edit.environments as Row[]) ?? [])];
                    environments[index] = { ...environment, recipient_allowlist: lines(e.target.value) };
                    setEdit({ ...edit, environments });
                  }} />
                </Field>
              </div>
            ))}
          </div>
        )}
      </Modal>
    </div>
  );
}

export function KeysPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({ project: "" });
  const query = useList("/keys", { ...filters.applied, offset: filters.offset });
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [revoke, setRevoke] = useState("");
  const [form, setForm] = useState({ project: "", mode: "sandbox", label: "", ttl_hours: "0", scopes: ["emails:send", "emails:read"] });
  const create = useMutation({
    mutationFn: () => send("post", "/keys", { ...form, ttl_hours: Number(form.ttl_hours) }) as Promise<{ key: string }>,
    onSuccess: (data) => {
      setSecret(data.key);
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const rotate = useMutation({
    mutationFn: (prefix: string) => send("post", `/keys/${prefix}/rotate`, {}) as Promise<{ key: string }>,
    onSuccess: (data) => {
      setSecret(data.key);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const revokeKey = useMutation({
    mutationFn: () => send("post", `/keys/${revoke}/revoke`),
    onSuccess: () => {
      toast.success("Key revoked.");
      setRevoke("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="API keys" description="The secret is shown once. The list keeps the prefix, scopes and last use.">
        {canWrite && <Button onClick={() => setOpen(true)}>Create key</Button>}
      </PageTitle>
      <form className="flex items-end gap-3" onSubmit={filters.apply}>
        <Field label="Project"><Input value={filters.draft.project} onChange={(e) => filters.setDraft({ project: e.target.value })} /></Field>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ServerTable
        columns={[
          { header: "Prefix", cell: (row) => show(row.prefix) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Status", cell: (row) => <StateBadge value={row.status} /> },
          { header: "Scopes", cell: (row) => show(row.scopes) },
          { header: "Last used", cell: (row) => <When value={row.last_used_at} /> },
          {
            header: "",
            cell: (row) => canWrite && row.status === "active" ? (
              <div className="flex gap-2">
                <Button size="sm" variant="outline" onClick={() => rotate.mutate(show(row.prefix))}>Rotate</Button>
                <Button size="sm" variant="destructive" onClick={() => setRevoke(show(row.prefix))}>Revoke</Button>
              </div>
            ) : null,
          },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No API keys."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={open} title="Create API key" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Create</Button>}>
        <div className="grid gap-3">
          <Field label="Project"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
          <Field label="Mode">
            <select className={controlClass} value={form.mode} onChange={(e) => setForm({ ...form, mode: e.target.value })}>
              <option value="sandbox">sandbox</option>
              <option value="live">live</option>
            </select>
          </Field>
          <Field label="Label"><Input value={form.label} onChange={(e) => setForm({ ...form, label: e.target.value })} /></Field>
          <Field label="TTL hours (0 = none)"><Input type="number" value={form.ttl_hours} onChange={(e) => setForm({ ...form, ttl_hours: e.target.value })} /></Field>
          <div className="grid gap-1 text-sm">
            {SCOPES.map((scope) => (
              <label key={scope} className="flex items-center gap-2">
                <input type="checkbox" checked={form.scopes.includes(scope)} onChange={(e) => setForm({
                  ...form,
                  scopes: e.target.checked ? [...form.scopes, scope] : form.scopes.filter((item) => item !== scope),
                })} />
                {scope}
              </label>
            ))}
          </div>
        </div>
      </Modal>
      <Modal open={secret !== ""} title="Copy this secret now" onClose={() => setSecret("")}>
        <p className="text-sm text-muted-foreground">It will not be shown again. Store it in the client application's server configuration.</p>
        <pre className="mt-3 overflow-x-auto rounded-md bg-muted p-3 text-xs">{secret}</pre>
        <div className="mt-3"><CopyButton value={secret} label="Copy secret" /></div>
      </Modal>
      <ConfirmDialog open={revoke !== ""} onOpenChange={(next) => !next && setRevoke("")} title="Revoke this key?" description="Requests that still use it will be denied." confirmLabel="Revoke" variant="destructive" onConfirm={() => revokeKey.mutate()} />
    </div>
  );
}

export function SendersPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({});
  const senders = useList("/senders", { offset: filters.offset });
  const dns = useQuery({ queryKey: ["admin", "dns"], queryFn: () => getJSON<{ items: Row[] }>("/dns") });
  const [open, setOpen] = useState(false);
  const [removeId, setRemoveId] = useState("");
  const [form, setForm] = useState({ project: "", mode: "live", email: "", display_name: "" });
  const create = useMutation({
    mutationFn: () => send("post", "/senders", form),
    onSuccess: () => {
      toast.success("Sender saved.");
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const remove = useMutation({
    mutationFn: () => send("delete", `/senders/${removeId}`),
    onSuccess: () => {
      setRemoveId("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const check = useMutation({
    mutationFn: () => send("post", "/dns/check"),
    onSuccess: () => {
      toast.success("DNS check finished.");
      void queryClient.invalidateQueries({ queryKey: ["admin", "dns"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-6">
      <PageTitle title="Senders and DNS" description="The checker is read-only. Fix SPF, DKIM, DMARC, MX and reverse DNS by hand.">
        {canWrite && (
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => check.mutate()} disabled={check.isPending}>Check DNS now</Button>
            <Button onClick={() => setOpen(true)}>Add sender</Button>
          </div>
        )}
      </PageTitle>
      <ServerTable
        columns={[
          { header: "Email", cell: (row) => show(row.email) },
          { header: "Name", cell: (row) => show(row.display_name) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "", cell: (row) => canWrite ? <Button size="sm" variant="destructive" onClick={() => setRemoveId(show(row.id))}>Delete</Button> : null },
        ]}
        rows={senders.data?.items}
        loading={senders.isLoading}
        error={senders.error}
        empty="No sender identities."
        offset={filters.offset}
        hasMore={senders.data?.has_more}
        onPage={filters.setOffset}
      />
      <div className="overflow-x-auto rounded-xl border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-left">
              <th className="p-3">Domain</th>
              <th className="p-3">Record</th>
              <th className="p-3">Result</th>
              <th className="p-3">Expected</th>
              <th className="p-3">Observed</th>
            </tr>
          </thead>
          <tbody>
            {(dns.data?.items ?? []).map((row, index) => (
              <tr key={index} className="border-b">
                <td className="p-3">{show(row.domain)}</td>
                <td className="p-3">{show(row.record_type)}</td>
                <td className="p-3">{row.pass === true ? "pass" : "fail"}</td>
                <td className="p-3">{show(row.expected)}</td>
                <td className="p-3">{show(row.observed)}</td>
              </tr>
            ))}
            {(dns.data?.items ?? []).length === 0 && <tr><td className="p-3 text-muted-foreground" colSpan={5}>No DNS checks yet.</td></tr>}
          </tbody>
        </table>
      </div>
      <Modal open={open} title="Add sender" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Save</Button>}>
        <div className="grid gap-3">
          <Field label="Project"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
          <Field label="Mode">
            <select className={controlClass} value={form.mode} onChange={(e) => setForm({ ...form, mode: e.target.value })}>
              <option value="sandbox">sandbox</option>
              <option value="live">live</option>
            </select>
          </Field>
          <Field label="Email"><Input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
          <Field label="Display name"><Input value={form.display_name} onChange={(e) => setForm({ ...form, display_name: e.target.value })} /></Field>
        </div>
      </Modal>
      <ConfirmDialog open={removeId !== ""} onOpenChange={(next) => !next && setRemoveId("")} title="Delete this sender?" description="The address can no longer be used as From for that environment." confirmLabel="Delete" variant="destructive" onConfirm={() => remove.mutate()} />
    </div>
  );
}

export default ProjectsPage;
