import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { admin, type Page } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
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
  const phrase = usePhrase();
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
        <h1 className="text-3xl font-bold">{phrase("Messages")}</h1>
        <p className="mt-2 text-muted-foreground">
          {phrase("Metadata only. Message content and recipients are not shown here.")}
        </p>
      </div>
      <label className="block text-sm">
        {phrase("State")}{" "}
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
        <p role="status">{phrase("Loading messages…")}</p>
      ) : query.error ? (
        <p role="alert">{query.error.message}</p>
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border bg-card">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b">
                  <th className="p-3">{phrase("Created")}</th>
                  <th className="p-3">{phrase("Project")}</th>
                  <th className="p-3">{phrase("Template")}</th>
                  <th className="p-3">{phrase("State")}</th>
                  <th className="p-3">{phrase("Attempts")}</th>
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
                    <td className="p-3">{m.template || phrase("Raw")}</td>
                    <td className="p-3">{m.state}</td>
                    <td className="p-3">{m.attempts}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!query.data.items.length && (
              <p className="p-5 text-muted-foreground">{phrase("No messages match this filter.")}</p>
            )}
          </div>
          <div className="flex gap-3">
            <button disabled={!offset} onClick={() => setOffset(Math.max(0, offset - 50))}>
              {phrase("Previous")}
            </button>
            <button disabled={!query.data.has_more} onClick={() => setOffset(offset + 50)}>
              {phrase("Next")}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
export function MessageDetail() {
  const phrase = usePhrase();
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
  if (q.isPending) return <p role="status">{phrase("Loading message…")}</p>;
  if (q.error) return <p role="alert">{q.error.message}</p>;
  const m = q.data;
  return (
    <div className="space-y-6">
      <Link to="/messages" className="text-primary">
        {phrase("← Messages")}
      </Link>
      <h1 className="text-3xl font-bold">
        {phrase("Messages")} {m.id}
      </h1>
      <section className="rounded-xl border bg-card p-5">
        <dl className="grid gap-4 sm:grid-cols-2">
          {[
            [phrase("State"), m.state],
            [phrase("Project"), m.project],
            [phrase("Environment"), m.mode],
            [phrase("Template"), m.template || phrase("Raw")],
            [phrase("Attempts"), String(m.attempts)],
            [phrase("Bounce state"), m.bounce_state || phrase("None")],
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
          <h2 className="text-xl font-semibold">{phrase("Resolve uncertain outcome")}</h2>
          <p className="text-sm text-muted-foreground">
            {phrase(
              "Check the mail server logs before resolving. This records the observed outcome; it does not send again.",
            )}
          </p>
          <select
            aria-label={phrase("Outcome")}
            value={resolution}
            onChange={(e) => setResolution(e.target.value)}
            className="rounded border bg-background p-2"
          >
            <option value="submitted">{phrase("Submitted")}</option>
            <option value="failed">{phrase("Failed")}</option>
          </select>
          <input
            aria-label={phrase("Evidence note")}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={phrase("Mail server log reference")}
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
                setActionError(e instanceof Error ? e.message : phrase("Resolution failed"));
              }
            }}
          >
            {phrase("Record resolution")}
          </button>
        </section>
      )}
      <section>
        <h2 className="text-xl font-semibold">{phrase("Event timeline")}</h2>
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
