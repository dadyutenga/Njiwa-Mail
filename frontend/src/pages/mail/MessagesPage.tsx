import { useState } from "react";
import { Link, useLocation, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Banner,
  controlClass,
  errText,
  Field,
  getJSON,
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

const STATES = ["", "queued", "scheduled", "submitting", "submitted", "retry_wait", "failed", "unknown", "expired", "captured", "cancelled"];

export function MessagesPage() {
  const [params] = useSearchParams();
  const location = useLocation();
  const initialState = location.pathname === "/scheduled" ? "scheduled" : (params.get("state") ?? "");
  const filters = useDraft({ project: "", mode: "", state: initialState, template: "", recipient: "", bounce_state: "" });
  const query = useList("/messages", { ...filters.applied, offset: filters.offset });
  return (
    <div className="space-y-4">
      <PageTitle title="Messages" description="Metadata only. Recipients, variables and bodies are not shown. Submitted is acceptance, not delivery." />
      <form className="flex flex-wrap items-end gap-3" onSubmit={filters.apply}>
        <Field label="Project"><Input value={filters.draft.project} onChange={(e) => filters.setDraft({ ...filters.draft, project: e.target.value })} /></Field>
        <Field label="Mode">
          <select className={controlClass} value={filters.draft.mode} onChange={(e) => filters.setDraft({ ...filters.draft, mode: e.target.value })}>
            <option value="">Any</option>
            <option value="sandbox">sandbox</option>
            <option value="live">live</option>
          </select>
        </Field>
        <Field label="State">
          <select className={controlClass} value={filters.draft.state} onChange={(e) => filters.setDraft({ ...filters.draft, state: e.target.value })}>
            {STATES.map((state) => <option key={state || "any"} value={state}>{state || "Any"}</option>)}
          </select>
        </Field>
        <Field label="Template"><Input value={filters.draft.template} onChange={(e) => filters.setDraft({ ...filters.draft, template: e.target.value })} /></Field>
        <Field label="Recipient"><Input placeholder="full address, not stored" value={filters.draft.recipient} onChange={(e) => filters.setDraft({ ...filters.draft, recipient: e.target.value })} /></Field>
        <Field label="Bounce">
          <select className={controlClass} value={filters.draft.bounce_state} onChange={(e) => filters.setDraft({ ...filters.draft, bounce_state: e.target.value })}>
            <option value="">Any</option>
            <option value="none">none</option>
            <option value="soft">soft</option>
            <option value="hard">hard</option>
          </select>
        </Field>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ServerTable
        columns={[
          { header: "State", cell: (row) => <StateBadge value={row.state} /> },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Template", cell: (row) => `${show(row.template)} ${show(row.locale)} v${show(row.template_version)}` },
          { header: "Bounce", cell: (row) => <StateBadge value={row.bounce_state} /> },
          { header: "Created", cell: (row) => <When value={row.created_at} /> },
          { header: "", cell: (row) => <Button variant="link" size="sm" asChild><Link to={`/messages/${show(row.id)}`}>Open</Link></Button> },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No messages match these filters."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
    </div>
  );
}

export function UnknownPage() {
  const canResolve = useCan("admin");
  const queryClient = useQueryClient();
  const [offset, setOffset] = useState(0);
  const query = useList("/messages", { state: "unknown", offset });
  const [note, setNote] = useState("");
  const [as, setAs] = useState("submitted");
  const [id, setId] = useState("");
  const resolve = useMutation({
    mutationFn: () => send("post", `/messages/${id}/resolve`, { as, note }),
    onSuccess: () => {
      toast.success("Marked resolved.");
      setId("");
      setNote("");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });
  return (
    <div className="space-y-4">
      <PageTitle title="Unknown queue" description="Acceptance may have happened. Reconcile against Mailcow, then mark the message with a note. It is never sent again automatically." />
      {canResolve && id && (
        <form className="grid max-w-xl gap-3 rounded-xl border p-4" onSubmit={(event) => { event.preventDefault(); resolve.mutate(); }}>
          <p className="text-sm">Resolving {id}</p>
          <Field label="Result">
            <select className={controlClass} value={as} onChange={(e) => setAs(e.target.value)}>
              <option value="submitted">submitted</option>
              <option value="failed">failed</option>
            </select>
          </Field>
          <Field label="Note"><Textarea required value={note} onChange={(e) => setNote(e.target.value)} placeholder="Mailcow queue id or log reference" /></Field>
          <Button type="submit" disabled={resolve.isPending || note.trim() === ""}>Mark resolved</Button>
        </form>
      )}
      <ServerTable
        columns={[
          { header: "Message", cell: (row) => show(row.id) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Template", cell: (row) => show(row.template) },
          { header: "Updated", cell: (row) => <When value={row.updated_at} /> },
          { header: "", cell: (row) => canResolve ? <Button size="sm" variant="outline" onClick={() => setId(show(row.id))}>Resolve</Button> : null },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No messages are waiting for reconciliation."
        offset={offset}
        hasMore={query.data?.has_more}
        onPage={setOffset}
      />
    </div>
  );
}

export function MessagePage() {
  const { id = "" } = useParams();
  const query = useQuery({
    queryKey: ["admin", "message", id],
    queryFn: () => getJSON<Row>(`/messages/${id}`),
    enabled: Boolean(id),
  });
  const message = query.data;
  const events = (message?.events as Row[] | undefined) ?? [];
  const bounces = (message?.bounces as Row[] | undefined) ?? [];
  return (
    <div className="space-y-4">
      <PageTitle title="Message" description="Timeline, attempts and bounce records. The body and variables stay on the server." />
      <Banner error={query.error} />
      {query.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {message && (
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="space-y-2 rounded-xl border p-4 text-sm">
            <p><StateBadge value={message.state} /></p>
            <p>Project {show(message.project)} / {show(message.mode)}</p>
            <p>Template {show(message.template)} version {show(message.template_version)} locale {show(message.locale)}</p>
            <p>Bounce <StateBadge value={message.bounce_state} /></p>
            <p>Priority {show(message.priority)} · Send at <When value={message.send_at} /></p>
            <p>Reply-to {show(message.reply_to)}</p>
            <p>Attempts {show(message.attempts)} · SMTP reply {show(message.smtp_reply)}</p>
            <p>Key prefix {show(message.api_key_prefix)}</p>
            <p>Created <When value={message.created_at} /> · Submitted <When value={message.submitted_at} /></p>
          </div>
          <div className="space-y-3">
            <h2 className="font-semibold">Events</h2>
            {events.length === 0 && <p className="text-sm text-muted-foreground">No events.</p>}
            {events.map((event, index) => (
              <div key={index} className="flex items-center justify-between gap-3 text-sm">
                <span>{show(event.type)} · {show(event.reason)}</span>
                <When value={event.at} />
              </div>
            ))}
            <h2 className="pt-2 font-semibold">Bounces</h2>
            {bounces.length === 0 && <p className="text-sm text-muted-foreground">No bounce recorded.</p>}
            {bounces.map((bounce) => (
              <div key={show(bounce.id)} className="text-sm">
                <StateBadge value={bounce.type} /> {show(bounce.status_code)} {show(bounce.recipient)}
                <p className="text-muted-foreground">{show(bounce.diagnostic)}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function BouncesPage() {
  const filters = useDraft({ type: "" });
  const query = useList("/bounces", { ...filters.applied, offset: filters.offset });
  return (
    <div className="space-y-4">
      <PageTitle title="Bounces" description="Hard bounces suppress the recipient. Policy bounces are recorded and never suppress." />
      <form className="flex items-end gap-3" onSubmit={filters.apply}>
        <Field label="Type">
          <select className={controlClass} value={filters.draft.type} onChange={(e) => filters.setDraft({ type: e.target.value })}>
            <option value="">Any</option>
            <option value="hard">hard</option>
            <option value="soft">soft</option>
            <option value="policy">policy</option>
            <option value="unknown">unknown</option>
          </select>
        </Field>
        <Button type="submit" variant="secondary">Filter</Button>
      </form>
      <ServerTable
        columns={[
          { header: "Type", cell: (row) => <StateBadge value={row.type} /> },
          { header: "Recipient", cell: (row) => show(row.recipient) },
          { header: "Status", cell: (row) => show(row.status_code) },
          { header: "Project", cell: (row) => `${show(row.project)} / ${show(row.mode)}` },
          { header: "Received", cell: (row) => <When value={row.received_at} /> },
          { header: "Message", cell: (row) => row.message_id ? <Link className="underline" to={`/messages/${show(row.message_id)}`}>Open</Link> : "—" },
        ]}
        rows={query.data?.items}
        loading={query.isLoading}
        error={query.error}
        empty="No bounces recorded."
        offset={filters.offset}
        hasMore={query.data?.has_more}
        onPage={filters.setOffset}
      />
    </div>
  );
}

export default MessagesPage;
