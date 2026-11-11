import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { admin } from "@/lib/admin-api";
type Overview = {
  counts: Record<string, number>;
  sent_today: number;
  queue_depth: number;
  webhook_backlog: number;
  open_alerts: number;
  smtp_configured: boolean;
  workers: { role: string; healthy: boolean; last_seen: string }[];
};
export default function Dashboard() {
  const q = useQuery({ queryKey: ["overview"], queryFn: () => admin.get<Overview>("/overview") });
  if (q.isPending) return <p role="status">Loading overview…</p>;
  if (q.error) return <p role="alert">{q.error.message}</p>;
  const d = q.data;
  const cards = [
    ["Submitted today", d.sent_today],
    ["Queue depth", d.queue_depth],
    ["Webhook backlog", d.webhook_backlog],
    ["Open alerts", d.open_alerts],
  ];
  return (
    <div className="space-y-8">
      <div>
        <p className="text-sm font-semibold text-primary">NJIWA-MAIL</p>
        <h1 className="mt-2 text-3xl font-bold">Overview</h1>
        <p className="mt-2 text-muted-foreground">
          Current operational data from the mail service.
        </p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {cards.map(([label, value]) => (
          <div className="rounded-xl border bg-card p-5" key={label}>
            <p className="text-sm text-muted-foreground">{label}</p>
            <p className="mt-3 text-3xl font-bold">{value}</p>
          </div>
        ))}
      </div>
      <div className="grid gap-5 lg:grid-cols-2">
        <section className="rounded-xl border bg-card p-5">
          <h2 className="font-semibold">Message states</h2>
          <dl className="mt-4 space-y-2">
            {Object.entries(d.counts).map(([state, count]) => (
              <div className="flex justify-between border-b py-2 text-sm" key={state}>
                <dt>{state}</dt>
                <dd>{count}</dd>
              </div>
            ))}
          </dl>
          <Link to="/messages" className="mt-4 inline-block text-sm text-primary">
            View messages →
          </Link>
        </section>
        <section className="rounded-xl border bg-card p-5">
          <h2 className="font-semibold">Service health</h2>
          <p className="mt-4 text-sm">
            SMTP: {d.smtp_configured ? "Configured" : "Not configured"}
          </p>
          <ul className="mt-3 space-y-2">
            {d.workers?.map((w) => (
              <li key={w.role + w.last_seen} className="text-sm">
                {w.role}: {w.healthy ? "Healthy" : "Unhealthy"}
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  );
}
