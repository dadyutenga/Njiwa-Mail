import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { admin, type Page } from "@/lib/admin-api";
type Message = {
  id: string;
  state: string;
  project: string;
  mode: string;
  template: string;
  created_at: string;
  attempts: number;
  bounce_state?: string;
  events?: { type: string; reason?: string; at: string }[];
  bounces?: { id: string; type: string; received_at: string }[];
};
export function Messages() {
  const [state, setState] = useState("");
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: ["messages", state, offset],
    queryFn: () =>
      admin.get<Page<Message>>(
        `/messages?state=${encodeURIComponent(state)}&limit=50&offset=${offset}`,
      ),
  });
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Messages</h1>
        <p className="mt-2 text-muted-foreground">
          Metadata only. Message content and recipients are not shown here.
        </p>
      </div>
      <label className="block text-sm">
        State{" "}
        <select
          className="ml-2 rounded border bg-background p-2"
          value={state}
          onChange={(e) => {
            setState(e.target.value);
            setOffset(0);
          }}
        >
          {[
            "",
            "queued",
            "scheduled",
            "submitting",
            "submitted",
            "retry_wait",
            "failed",
            "unknown",
            "expired",
            "captured",
            "cancelled",
          ].map((x) => (
            <option key={x} value={x}>
              {x || "All"}
            </option>
          ))}
        </select>
      </label>
      {query.isPending ? (
        <p role="status">Loading messages…</p>
      ) : query.error ? (
        <p role="alert">{query.error.message}</p>
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border bg-card">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b">
                  <th className="p-3">Created</th>
                  <th className="p-3">Project</th>
                  <th className="p-3">Template</th>
                  <th className="p-3">State</th>
                  <th className="p-3">Attempts</th>
                </tr>
              </thead>
              <tbody>
                {query.data.items.map((m) => (
                  <tr key={m.id} className="border-b">
                    <td className="p-3">
                      <Link className="text-primary hover:underline" to={`/messages/${m.id}`}>
                        {new Date(m.created_at).toLocaleString()}
                      </Link>
                    </td>
                    <td className="p-3">
                      {m.project} / {m.mode}
                    </td>
                    <td className="p-3">{m.template || "Raw"}</td>
                    <td className="p-3">{m.state}</td>
                    <td className="p-3">{m.attempts}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!query.data.items.length && (
              <p className="p-5 text-muted-foreground">No messages match this filter.</p>
            )}
          </div>
          <div className="flex gap-3">
            <button disabled={!offset} onClick={() => setOffset(Math.max(0, offset - 50))}>
              Previous
            </button>
            <button disabled={!query.data.has_more} onClick={() => setOffset(offset + 50)}>
              Next
            </button>
          </div>
        </>
      )}
    </div>
  );
}
export function MessageDetail() {
  const { id } = useParams();
  const client = useQueryClient();
  const [resolution, setResolution] = useState("submitted");
  const [note, setNote] = useState("");
  const [actionError, setActionError] = useState("");
  const q = useQuery({
    queryKey: ["message", id],
    queryFn: () => admin.get<Message>(`/messages/${encodeURIComponent(id || "")}`),
    enabled: !!id,
  });
  if (q.isPending) return <p role="status">Loading message…</p>;
  if (q.error) return <p role="alert">{q.error.message}</p>;
  const m = q.data;
  return (
    <div className="space-y-6">
      <Link to="/messages" className="text-primary">
        ← Messages
      </Link>
      <h1 className="text-3xl font-bold">Message {m.id}</h1>
      <section className="rounded-xl border bg-card p-5">
        <dl className="grid gap-4 sm:grid-cols-2">
          {[
            ["State", m.state],
            ["Project", m.project],
            ["Environment", m.mode],
            ["Template", m.template || "Raw"],
            ["Attempts", String(m.attempts)],
            ["Bounce state", m.bounce_state || "None"],
          ].map(([k, v]) => (
            <div key={k}>
              <dt className="text-sm text-muted-foreground">{k}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
      </section>
      {m.state === "unknown" && (
        <section className="space-y-3 rounded-xl border bg-card p-5">
          <h2 className="text-xl font-semibold">Resolve uncertain outcome</h2>
          <p className="text-sm text-muted-foreground">
            Check the mail server logs before resolving. This records the observed outcome; it does
            not send again.
          </p>
          <select
            aria-label="Outcome"
            value={resolution}
            onChange={(e) => setResolution(e.target.value)}
            className="rounded border bg-background p-2"
          >
            <option value="submitted">Submitted</option>
            <option value="failed">Failed</option>
          </select>
          <input
            aria-label="Evidence note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Mail server log reference"
            className="ml-3 rounded border bg-background p-2"
          />
          {actionError && (
            <p role="alert" className="text-destructive">
              {actionError}
            </p>
          )}
          <button
            className="block rounded bg-primary px-4 py-2 text-primary-foreground"
            onClick={async () => {
              try {
                await admin.send(`/messages/${encodeURIComponent(id || "")}/resolve`, "POST", {
                  as: resolution,
                  note,
                });
                client.invalidateQueries({ queryKey: ["message", id] });
                client.invalidateQueries({ queryKey: ["messages"] });
              } catch (e) {
                setActionError(e instanceof Error ? e.message : "Resolution failed");
              }
            }}
          >
            Record resolution
          </button>
        </section>
      )}
      <section>
        <h2 className="text-xl font-semibold">Event timeline</h2>
        <ol className="mt-3 space-y-3">
          {m.events?.map((e, i) => (
            <li key={i} className="rounded-lg border p-4">
              {e.type} {e.reason && `· ${e.reason}`}{" "}
              <time className="block text-sm text-muted-foreground">
                {new Date(e.at).toLocaleString()}
              </time>
            </li>
          ))}
        </ol>
      </section>
    </div>
  );
}
