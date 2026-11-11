import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { admin } from "@/lib/admin-api";
type Inbound = {
  id: string;
  subject: string;
  header_from: string;
  envelope_to: string;
  text: string;
  html: string;
  attachments?: { id: string; filename: string }[];
};
type Capture = { id: string; subject: string; from: string; to: string; text: string; raw: string };
export default function ResourceDetail({ kind }: { kind: "inbound" | "sandbox" }) {
  const { id } = useParams();
  const q = useQuery({
    queryKey: [kind, id],
    queryFn: () => admin.get<Inbound | Capture>(`/${kind}/${encodeURIComponent(id || "")}`),
    enabled: !!id,
  });
  if (q.isPending) return <p role="status">Loading…</p>;
  if (q.error) return <p role="alert">{q.error.message}</p>;
  const data = q.data;
  const inbound = kind === "inbound" ? (data as Inbound) : null;
  const capture = kind === "sandbox" ? (data as Capture) : null;
  return (
    <div className="space-y-6">
      <Link className="text-primary" to={`/${kind}`}>
        ← {kind}
      </Link>
      <h1 className="text-3xl font-bold">{data.subject || data.id}</h1>
      <section className="rounded-xl border bg-card p-5">
        <p>
          <strong>From:</strong> {inbound?.header_from || capture?.from}
        </p>
        <p>
          <strong>To:</strong> {inbound?.envelope_to || capture?.to}
        </p>
      </section>
      <section className="rounded-xl border bg-card p-5">
        <h2 className="font-semibold">Text content</h2>
        <pre className="mt-3 whitespace-pre-wrap break-words text-sm">
          {data.text || capture?.raw || "No text content"}
        </pre>
      </section>
      {inbound?.attachments?.length && (
        <section className="rounded-xl border bg-card p-5">
          <h2 className="font-semibold">Attachments</h2>
          <ul>
            {inbound.attachments.map((a) => (
              <li key={a.id}>
                <a
                  className="text-primary underline"
                  href={`/admin/v1/inbound/${encodeURIComponent(id || "")}/attachments/${encodeURIComponent(a.id)}`}
                >
                  {a.filename}
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
