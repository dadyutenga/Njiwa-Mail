import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { BarChartWrapper } from "@/components/shared/Charts";
import { ChartCard } from "@/components/shared/ChartCard";
import {
  controlClass,
  errText,
  Field,
  getJSON,
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

export function DomainsPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const filters = useDraft({});
  const query = useList("/domains", { offset: filters.offset });
  const [form, setForm] = useState({ project: "", name: "", purpose: "sending" });
  const [txt, setTxt] = useState("");
  const create = useMutation({
    mutationFn: () => send("post", "/domains", form) as Promise<{ txt: string }>,
    onSuccess: (data) => {
      setTxt(data.txt);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  const verify = useMutation({
    mutationFn: (id: string) => send("post", `/domains/${id}/verify`),
    onSuccess: () => {
      toast.success("Verification checked.");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Domains" description="Live sending stays blocked until the TXT challenge, SPF, DKIM and DMARC all pass. The checker does not change DNS." />
      {txt && <p className="rounded-md border p-3 font-mono text-xs">Publish this TXT record, then verify: {txt}</p>}
      {canWrite && (
        <form className="flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}>
          <Field label="Project"><Input value={form.project} onChange={(e) => setForm({ ...form, project: e.target.value })} /></Field>
          <Field label="Domain"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="notify.example.com" /></Field>
          <Field label="Purpose">
            <select className={controlClass} value={form.purpose} onChange={(e) => setForm({ ...form, purpose: e.target.value })}>
              <option value="sending">sending</option>
              <option value="inbound">inbound</option>
            </select>
          </Field>
          <Button type="submit">Add domain</Button>
        </form>
      )}
      <ServerTable
        columns={[
          { header: "Domain", cell: (row) => show(row.name) },
          { header: "Purpose", cell: (row) => show(row.purpose) },
          { header: "SPF", cell: (row) => show(row.spf_ok) },
          { header: "DKIM", cell: (row) => show(row.dkim_ok) },
          { header: "DMARC", cell: (row) => show(row.dmarc_ok) },
          { header: "Verified", cell: (row) => <When value={row.verified_at} /> },
          { header: "", cell: (row) => canWrite ? <Button size="sm" variant="outline" onClick={() => verify.mutate(show(row.id))}>Verify</Button> : null },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No domains yet. Live mail from an unregistered domain is unchanged."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
    </div>
  );
}

export function AnalyticsPage() {
  const query = useQuery({ queryKey: ["admin", "analytics"], queryFn: () => getJSON<{ items: Row[] }>("/analytics") });
  const rows = query.data?.items ?? [];
  const bars = rows.map((row) => ({
    name: show(row.day),
    submitted: Number(row.submitted ?? 0),
    failed: Number(row.failed ?? 0),
  }));
  return (
    <div className="space-y-4">
      <PageTitle title="Analytics" description="Counts come from recorded message states. Submitted means the SMTP server accepted the message." />
      <ChartCard title="Sends by day" description="Africa/Dar es Salaam" isLoading={query.isLoading}>
        {bars.length > 0 ? (
          <BarChartWrapper data={bars} bars={[{ key: "submitted", name: "submitted" }, { key: "failed", name: "failed", color: "hsl(0 72% 51%)" }]} />
        ) : (
          <p className="py-10 text-center text-sm text-muted-foreground">No messages in range.</p>
        )}
      </ChartCard>
    </div>
  );
}

export function AlertsPage() {
  const canWrite = useCan("admin");
  const queryClient = useQueryClient();
  const alerts = useList("/alerts", { offset: 0 });
  const channels = useQuery({ queryKey: ["admin", "channels"], queryFn: () => getJSON<{ items: Row[] }>("/alert-channels") });
  const [form, setForm] = useState({ kind: "email", target: "" });
  const save = useMutation({
    mutationFn: () => send("post", "/alert-channels", form),
    onSuccess: () => {
      setForm({ ...form, target: "" });
      void queryClient.invalidateQueries({ queryKey: ["admin", "channels"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Alerts" description="Queue, bounce, DNS and unknown-message alerts. Channels receive the operator summary." />
      {canWrite && (
        <form className="flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}>
          <Field label="Kind">
            <select className={controlClass} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
              <option value="email">email</option>
              <option value="webhook">webhook</option>
            </select>
          </Field>
          <Field label="Target"><Input value={form.target} onChange={(e) => setForm({ ...form, target: e.target.value })} /></Field>
          <Button type="submit">Add channel</Button>
        </form>
      )}
      <ServerTable
        columns={[
          { header: "Kind", cell: (row) => show(row.kind) },
          { header: "Summary", cell: (row) => show(row.summary || row.target) },
          { header: "When", cell: (row) => <When value={row.created_at} /> },
        ]}
        rows={[...(channels.data?.items ?? []), ...(alerts.data?.items ?? [])]}
        loading={alerts.isLoading}
        error={alerts.error}
        empty="No alerts or channels."
        offset={0}
        hasMore={false}
        onPage={() => undefined}
      />
    </div>
  );
}

export function SecurityPage() {
  const queryClient = useQueryClient();
  const sessions = useQuery({ queryKey: ["admin", "sessions"], queryFn: () => getJSON<{ items: Row[] }>("/auth/sessions") });
  const [url, setUrl] = useState("");
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState<string[]>([]);
  const begin = useMutation({
    mutationFn: () => send("post", "/auth/totp") as Promise<{ otpauth_url: string }>,
    onSuccess: (data) => setUrl(data.otpauth_url),
    onError: (error) => toast.error(errText(error)),
  });
  const confirm = useMutation({
    mutationFn: () => send("post", "/auth/totp/confirm", { code }) as Promise<{ recovery_codes: string[] }>,
    onSuccess: (data) => {
      setRecovery(data.recovery_codes);
      toast.success("Second factor enabled.");
    },
    onError: (error) => toast.error(errText(error)),
  });
  const revoke = useMutation({
    mutationFn: (id: string) => send("delete", `/auth/sessions/${id}`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["admin", "sessions"] }),
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Security" description="Owners and admins enroll a time-based second factor. Recovery codes are shown once. Signing out a session is immediate." />
      <div className="grid max-w-xl gap-3 rounded-xl border p-4">
        <Button variant="outline" onClick={() => begin.mutate()}>Start authenticator setup</Button>
        {url && <p className="break-all font-mono text-xs">{url}</p>}
        <Field label="Code from the authenticator"><Input value={code} onChange={(e) => setCode(e.target.value)} /></Field>
        <Button onClick={() => confirm.mutate()} disabled={code.length < 6}>Confirm</Button>
        {recovery.length > 0 && <pre className="text-xs">{recovery.join("\n")}</pre>}
      </div>
      <ServerTable
        columns={[
          { header: "IP", cell: (row) => show(row.ip) },
          { header: "Client", cell: (row) => show(row.user_agent) },
          { header: "Started", cell: (row) => <When value={row.created_at} /> },
          { header: "", cell: (row) => <Button size="sm" variant="destructive" onClick={() => revoke.mutate(show(row.id))}>Sign out</Button> },
        ]}
        rows={sessions.data?.items}
        loading={sessions.isLoading}
        error={sessions.error}
        empty="No sessions."
        offset={0}
        hasMore={false}
        onPage={() => undefined}
      />
    </div>
  );
}

export function DocsPage() {
  const query = useQuery({ queryKey: ["admin", "docs"], queryFn: () => getJSON<{ operations: Row[] }>("/docs") });
  return (
    <div className="space-y-4">
      <PageTitle title="API docs" description="The application API is /v1. These operations are the ones the server exposes." />
      <ServerTable
        columns={[
          { header: "Method", cell: (row) => show(row.method) },
          { header: "Path", cell: (row) => show(row.path) },
          { header: "Purpose", cell: (row) => show(row.purpose) },
        ]}
        rows={query.data?.operations}
        loading={query.isLoading}
        error={query.error}
        empty="No operations."
        offset={0}
        hasMore={false}
        onPage={() => undefined}
      />
    </div>
  );
}

export default DomainsPage;
