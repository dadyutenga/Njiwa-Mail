import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ChartCard } from "@/components/shared/ChartCard";
import { BarChartWrapper, PieChartWrapper } from "@/components/shared/Charts";
import { StatCard } from "@/components/shared/StatCard";
import { useOverviewQuery } from "@/features/console/mailApi";

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

export function ReportsPage() {
  const overview = useOverviewQuery();
  const counts = overview.data?.counts ?? {};
  const bars = Object.entries(counts).map(([name, count]) => ({ name, count }));
  const pie = Object.entries(counts)
    .filter(([, value]) => value > 0)
    .map(([name, value], index) => ({ name, value, color: COLORS[index % COLORS.length] }));
  const terminal = (counts.failed ?? 0) + (counts.expired ?? 0) + (counts.unknown ?? 0);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Queue</h1>
          <p className="text-sm text-muted-foreground">Live counts from the mail service. There is no delivery or open tracking.</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => overview.refetch()} disabled={overview.isFetching} className="gap-1.5">
          <RefreshCw className={`h-4 w-4 ${overview.isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {overview.isError && (
        <div className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          {overview.error.message}
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard title="Total" value={overview.data?.messages ?? "—"} />
        <StatCard title="In flight" value={(counts.queued ?? 0) + (counts.submitting ?? 0) + (counts.retry_wait ?? 0)} description="Queued, submitting, or waiting to retry" />
        <StatCard title="Submitted" value={counts.submitted ?? 0} description="SMTP accepted" />
        <StatCard title="Needs attention" value={terminal} description="Failed, expired, or unknown" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <ChartCard title="Counts" description="Every state, including zeros" className="lg:col-span-2">
          <BarChartWrapper data={bars} bars={[{ key: "count", name: "Messages", color: "hsl(187 85% 43%)" }]} />
        </ChartCard>
        <ChartCard title="Share" description="States with at least one message">
          {pie.length > 0 ? (
            <PieChartWrapper data={pie} />
          ) : (
            <p className="py-16 text-center text-sm text-muted-foreground">No messages yet.</p>
          )}
        </ChartCard>
      </div>
    </div>
  );
}

export default ReportsPage;
