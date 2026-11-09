import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, AlertTriangle, Inbox, Mail, Server } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { StatCard } from "@/components/shared/StatCard";
import { ChartCard } from "@/components/shared/ChartCard";
import { BarChartWrapper, PieChartWrapper } from "@/components/shared/Charts";
import { Banner, errText, getJSON, send, show, StateBadge, useCan, When, type Row } from "@/components/admin/bits";

interface Overview {
  counts: Record<string, number>;
  sent_today: number;
  queue_depth: number;
  oldest_queued_age_seconds: number;
  webhook_backlog: number;
  webhook_dead: number;
  inbound_spool_pending: number;
  open_alerts: number;
  dns_ok: boolean | null;
  smtp_configured: boolean;
  inbound_domain: string;
  bounce_address: string;
  bounces: { hard_percent: number; policy_percent: number; submitted_last_hour: number };
  workers: { role: string; instance: string; last_seen: string; healthy: boolean }[];
}

const COLORS = [
  "hsl(187 85% 43%)",
  "hsl(45 90% 50%)",
  "hsl(142 70% 45%)",
  "hsl(25 95% 65%)",
  "hsl(0 72% 51%)",
  "hsl(280 65% 60%)",
  "hsl(215 15% 60%)",
  "hsl(200 70% 50%)",
];

function age(seconds: number): string {
  if (!seconds || seconds < 1) return "0s";
  if (seconds < 90) return `${Math.round(seconds)}s`;
  if (seconds < 5400) return `${Math.round(seconds / 60)}m`;
  return `${(seconds / 3600).toFixed(1)}h`;
}

export function OverviewPage() {
  const canAck = useCan("admin");
  const queryClient = useQueryClient();
  const overview = useQuery({ queryKey: ["admin", "overview"], queryFn: () => getJSON<Overview>("/overview"), refetchInterval: 15_000 });
  const alerts = useQuery({
    queryKey: ["admin", "alerts"],
    queryFn: () => getJSON<{ items: Row[] }>("/alerts", { open: "true", limit: 10 }),
    refetchInterval: 15_000,
  });
  const ack = useMutation({
    mutationFn: (id: string) => send("post", `/alerts/${id}/ack`),
    onSuccess: () => {
      toast.success("Alert acknowledged.");
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (error) => toast.error(errText(error)),
  });

  const data = overview.data;
  const counts = data?.counts ?? {};
  const bars = Object.entries(counts).map(([name, count]) => ({ name, count }));
  const pie = Object.entries(counts)
    .filter(([, value]) => value > 0)
    .map(([name, value], index) => ({ name, value, color: COLORS[index % COLORS.length] }));
  const dns = data?.dns_ok == null ? "Not checked" : data.dns_ok ? "Pass" : "Fail";

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Overview</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Times are Africa/Dar es Salaam. Hover a time to see UTC. Submitted means Mailcow accepted the message. There is no delivered state.
        </p>
      </div>
      <Banner error={overview.error} />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard title="Sent today" value={data?.sent_today ?? "—"} icon={Mail} description="Submitted since midnight" />
        <StatCard title="Queue depth" value={data?.queue_depth ?? "—"} icon={Inbox} description={`Oldest ${data ? age(data.oldest_queued_age_seconds) : "—"}`} />
        <StatCard title="Hard bounce rate" value={data ? `${data.bounces.hard_percent.toFixed(1)}%` : "—"} icon={AlertTriangle} description={`Policy ${data ? data.bounces.policy_percent.toFixed(1) : "—"}% last hour`} />
        <StatCard title="DNS" value={dns} icon={Server} description={data?.smtp_configured ? "SMTP configured" : "SMTP not configured"} />
      </div>
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <ChartCard title="Messages by state" description="Exact backend states" className="lg:col-span-2" isLoading={overview.isLoading}>
          <BarChartWrapper data={bars} bars={[{ key: "count", name: "Messages", color: "hsl(187 85% 43%)" }]} />
        </ChartCard>
        <ChartCard title="State mix" description="States that currently have messages" isLoading={overview.isLoading}>
          {pie.length > 0 ? <PieChartWrapper data={pie} innerRadius={65} outerRadius={95} /> : <p className="py-16 text-center text-sm text-muted-foreground">No messages yet.</p>}
        </ChartCard>
      </div>
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <ChartCard title="Workers" description={`Webhook backlog ${data?.webhook_backlog ?? "—"} · dead ${data?.webhook_dead ?? "—"} · inbound spool ${data?.inbound_spool_pending ?? "—"}`}>
          <div className="space-y-2 text-sm">
            {(data?.workers ?? []).length === 0 && <p className="text-muted-foreground">No heartbeats yet.</p>}
            {data?.workers.map((worker) => (
              <div key={`${worker.role}-${worker.instance}`} className="flex items-center justify-between gap-3">
                <span className="font-medium">{worker.role}</span>
                <StateBadge value={worker.healthy ? "active" : "unknown"} />
                <span className="text-muted-foreground"><When value={worker.last_seen} /></span>
              </div>
            ))}
            <p className="pt-2 text-xs text-muted-foreground">
              Inbound domain {data?.inbound_domain || "not set"} · bounce address {data?.bounce_address || "not set"}
            </p>
          </div>
        </ChartCard>
        <ChartCard title="Open alerts" description="Policy bounces and rate thresholds" action={<Activity className="h-4 w-4 text-muted-foreground" />}>
          <div className="space-y-3 text-sm">
            {(alerts.data?.items ?? []).length === 0 && <p className="text-muted-foreground">No open alerts.</p>}
            {alerts.data?.items.map((alert) => (
              <div key={show(alert.id)} className="flex items-start justify-between gap-3">
                <div>
                  <p className="font-medium">{show(alert.kind)}</p>
                  <p className="text-muted-foreground">{show(alert.summary)}</p>
                </div>
                {canAck && (
                  <Button size="sm" variant="outline" onClick={() => ack.mutate(show(alert.id))} disabled={ack.isPending}>
                    Acknowledge
                  </Button>
                )}
              </div>
            ))}
          </div>
        </ChartCard>
      </div>
    </div>
  );
}

export default OverviewPage;
