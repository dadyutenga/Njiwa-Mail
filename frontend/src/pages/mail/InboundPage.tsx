import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import {
  Banner,
  controlClass,
  errText,
  Field,
  getJSON,
  Modal,
  PageTitle,
  send,
  ServerTable,
  show,
  useCan,
  useDraft,
  useList,
  When,
  type Row,
} from "@/components/admin/bits";

export function InboundPage() {
  const filters = useDraft({ project: "", mode: "" });
  const query = useList("/inbound", { ...filters.applied, offset: filters.offset });
  return (
    <div className="space-y-4">
      <PageTitle title="Inbound" description="Parsed mail for in.<domain>. HTML is shown in a sandboxed frame." />
      <form className="flex flex-wrap items-end gap-3" onSubmit={filters.apply}>
        <Field label="Project"><Input value={filters.draft.project} onChange={(e) => filters.setDraft({ ...filters.draft, project: e.target.value })} /></Field>
        <Field label="Mode">
          <select className={controlClass} value={filters.draft.mode} onChange={(e) => filters.setDraft({ ...filters.draft, mode: e.target.value })}>
            <option value="">Any</option>
            <option value="sandbox">sandbox</option>
            <option value="live">live</option>
          </select>
        </Field>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ServerTable
        columns={[
          { header: "Subject", cell: (row) => show(row.subject) },
          { header: "From", cell: (row) => show(row.header_from) },
          { header: "To", cell: (row) => show(row.envelope_to) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Spam", cell: (row) => show(row.spam_score) },
          { header: "Received", cell: (row) => <When value={row.received_at} /> },
          { header: "", cell: (row) => <Button variant="link" size="sm" asChild><Link to={`/inbound/${show(row.id)}`}>Open</Link></Button> },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No inbound mail."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
    </div>
  );
}

export function InboundDetailPage() {
  const { id = "" } = useParams();
  const canRead = useCan("admin");
  const [html, setHtml] = useState(false);
  const query = useQuery({
    queryKey: ["admin", "inbound", id],
    queryFn: () => getJSON<Row>(`/inbound/${id}`),
    enabled: Boolean(id),
  });
  const message = query.data;
  const attachments = (message?.attachments as Row[] | undefined) ?? [];
  return (
    <div className="space-y-4">
      <PageTitle title="Inbound message" description="Text is the default view. HTML never runs scripts or loads remote content." />
      <Banner error={query.error} />
      {message && (
        <div className="space-y-4">
          <div className="space-y-1 text-sm">
            <p className="font-medium">{show(message.subject)}</p>
            <p>From {show(message.header_from)} · envelope {show(message.envelope_from)} → {show(message.envelope_to)}</p>
            <p>{show(message.project)} / {show(message.mode)} · spam {show(message.spam_score)} · <When value={message.received_at} /></p>
          </div>
          {canRead ? (
            <>
              <div className="flex gap-2">
                <Button size="sm" variant={html ? "outline" : "default"} onClick={() => setHtml(false)}>Text</Button>
                <Button size="sm" variant={html ? "default" : "outline"} onClick={() => setHtml(true)}>HTML</Button>
              </div>
              {html ? (
                <iframe title="Inbound HTML" sandbox="" className="h-[480px] w-full rounded-md border bg-white" src={`/admin/v1/inbound/${id}/html`} />
              ) : (
                <pre className="whitespace-pre-wrap rounded-xl border p-4 text-sm">{show(message.text)}</pre>
              )}
              <div className="space-y-2">
                {attachments.map((file) => (
                  <a key={show(file.id)} className="block text-sm underline" href={`/admin/v1/inbound/${id}/attachments/${show(file.id)}`}>
                    {show(file.filename)} ({show(file.size)} bytes)
                  </a>
                ))}
              </div>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">Inbound bodies are available to admins and owners.</p>
          )}
        </div>
      )}
    </div>
  );
}

export function AddressesPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({});
  const query = useList("/inbound-addresses", { offset: filters.offset });
  const [open, setOpen] = useState(false);
  const [removeId, setRemoveId] = useState("");
  const [form, setForm] = useState({ project: "", mode: "live", local_part: "", webhook_endpoint_id: "" });
  const create = useMutation({
    mutationFn: () => send("post", "/inbound-addresses", {
      project: form.project,
      mode: form.mode,
      local_part: form.local_part,
      webhook_endpoint_id: form.webhook_endpoint_id || null,
    }),
    onSuccess: () => {
      toast.success("Address created.");
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const toggle = useMutation({
    mutationFn: (row: Row) => send("patch", `/inbound-addresses/${show(row.id)}`, { enabled: row.enabled !== true }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["admin"] }),
    onError: (error) => toast.error(errText(error)),
  });
  const remove = useMutation({
    mutationFn: () => send("delete", `/inbound-addresses/${removeId}`),
    onSuccess: () => {
      toast.success("Address deleted.");
      setRemoveId("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Inbound addresses" description="Local parts under the inbound domain. * is the wildcard for addresses that have no exact match.">
        {canWrite && <Button onClick={() => setOpen(true)}>Add address</Button>}
      </PageTitle>
      <ServerTable
        columns={[
          { header: "Address", cell: (row) => show(row.address) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Enabled", cell: (row) => show(row.enabled) },
          { header: "Webhook", cell: (row) => show(row.webhook_endpoint_id) },
          {
            header: "",
            cell: (row) => canWrite ? (
              <div className="flex gap-2">
                <Button size="sm" variant="outline" onClick={() => toggle.mutate(row)}>{row.enabled === true ? "Disable" : "Enable"}</Button>
                <Button size="sm" variant="destructive" onClick={() => setRemoveId(show(row.id))}>Delete</Button>
              </div>
            ) : null,
          },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No inbound addresses."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
      <Modal open={open} title="Add inbound address" onClose={() => setOpen(false)} footer={<Button onClick={() => create.mutate()} disabled={create.isPending}>Create</Button>}>
        <div className="grid gap-3">
          <Field label="Project"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
          <Field label="Mode">
            <select className={controlClass} value={form.mode} onChange={(e) => setForm({ ...form, mode: e.target.value })}>
              <option value="sandbox">sandbox</option>
              <option value="live">live</option>
            </select>
          </Field>
          <Field label="Local part"><Input value={form.local_part} onChange={(e) => setForm({ ...form, local_part: e.target.value })} placeholder="support or *" /></Field>
          <Field label="Webhook endpoint id"><Input value={form.webhook_endpoint_id} onChange={(e) => setForm({ ...form, webhook_endpoint_id: e.target.value })} /></Field>
        </div>
      </Modal>
      <ConfirmDialog
        open={removeId !== ""}
        onOpenChange={(next) => !next && setRemoveId("")}
        title="Delete this address?"
        description="Mail to this address will be rejected at SMTP until you add it again."
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={() => remove.mutate()}
      />
    </div>
  );
}

export default InboundPage;
