import { Link } from "react-router-dom";
import { Activity, ArrowRight, Inbox, Layers, Mail, Server } from "lucide-react";
import { Button } from "@/components/ui/button";
import { StatCard } from "@/components/shared/StatCard";
import { ChartCard } from "@/components/shared/ChartCard";
import { BarChartWrapper, PieChartWrapper } from "@/components/shared/Charts";
import { ActivityFeed } from "@/components/shared/ActivityFeed";
import { useAuth } from "@/contexts/AuthContext";
import { formatDate } from "@/lib/utils";
import { useMessagesQuery, useOverviewQuery } from "@/features/console/mailApi";
import type { ActivityItem } from "@/types/dashboard";

const STATE_COLORS = [
  "hsl(187 85% 43%)",
  "hsl(45 90% 50%)",
  "hsl(142 70% 45%)",
  "hsl(25 95% 65%)",
  "hsl(0 72% 51%)",
  "hsl(280 65% 60%)",
  "hsl(215 15% 60%)",
  "hsl(200 70% 50%)",
];

function activityType(state: string): ActivityItem["type"] {
  if (state === "failed" || state === "expired") return "delete";
  if (state === "submitted" || state === "captured") return "update";
  return "create";
}

export function DashboardPage() {
  const { user } = useAuth();
  const today = formatDate(new Date(), "EEEE, MMMM do, yyyy");
  const overview = useOverviewQuery();
  const messages = useMessagesQuery();

  const counts = overview.data?.counts ?? {};
  const pie = Object.entries(counts)
    .filter(([, value]) => value > 0)
    .map(([name, value], index) => ({
      name,
      value,
      color: STATE_COLORS[index % STATE_COLORS.length],
    }));
  const bars = Object.entries(counts).map(([name, count]) => ({ name, count }));
  const feed: ActivityItem[] = (messages.data ?? []).slice(0, 6).map((message) => ({
    id: message.id,
    user: { name: `${message.project} / ${message.mode}` },
    action: message.state,
    target: `${message.template} · ${message.id.slice(0, 8)}`,
    timestamp: message.created_at,
    type: activityType(message.state),
  }));

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 rounded-2xl bg-gradient-to-r from-primary/10 via-primary/5 to-transparent p-6 border border-primary/20">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
            {user?.name || "Operator"}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Queue and project status for {today}. Submitted means the SMTP server accepted the message.
          </p>
        </div>
        <Button size="sm" asChild>
          <Link to="/messages" className="gap-1.5">
            <Mail className="h-4 w-4" />
            <span>Messages</span>
          </Link>
        </Button>
      </div>

      {overview.isError && (
        <div className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          {overview.error.message || "The API did not respond."}
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard title="Messages" value={overview.data?.messages ?? "—"} icon={Inbox} description="Metadata retained by the service" />
        <StatCard title="Queued" value={counts.queued ?? "—"} icon={Layers} description="Waiting for the worker" />
        <StatCard title="Submitted" value={counts.submitted ?? "—"} icon={Mail} description="Accepted by SMTP, not delivered" />
        <StatCard
          title="Worker"
          value={overview.data ? (overview.data.worker.healthy ? "Healthy" : "No heartbeat") : "—"}
          icon={Server}
          description={overview.data?.smtp_configured ? "SMTP configured" : "Sandbox only"}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <ChartCard title="Messages by state" description="Current queue, from the API" className="lg:col-span-2">
          <BarChartWrapper data={bars} bars={[{ key: "count", name: "Messages", color: "hsl(187 85% 43%)" }]} />
        </ChartCard>
        <ChartCard title="State mix" description="States that currently have messages">
          {pie.length > 0 ? (
            <PieChartWrapper data={pie} innerRadius={65} outerRadius={95} />
          ) : (
            <p className="py-16 text-center text-sm text-muted-foreground">No messages yet.</p>
          )}
        </ChartCard>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <ChartCard
          title="Recent messages"
          description="Project, environment, and state. No recipients or message content."
          className="lg:col-span-2"
          action={
            <Button variant="ghost" size="sm" asChild className="text-xs h-7 gap-1">
              <Link to="/messages">
                Open the list <ArrowRight className="h-3.5 w-3.5" />
              </Link>
            </Button>
          }
        >
          <div className="pt-2">
            <ActivityFeed items={feed} emptyMessage="No messages have been accepted yet." />
          </div>
        </ChartCard>
        <ChartCard
          title="Projects"
          description="Provisioning stays on the operator CLI"
          action={
            <div className="flex items-center gap-1 text-xs text-muted-foreground">
              <Activity className="h-3.5 w-3.5" />
              <span>{overview.data?.projects ?? 0}</span>
            </div>
          }
        >
          <div className="space-y-3 pt-2 text-sm text-muted-foreground">
            <p>Create projects, senders, and API keys with the ditronics-mail CLI. This console only reads status.</p>
            <Button variant="link" size="sm" asChild className="p-0 h-auto text-xs">
              <Link to="/projects">View projects and key prefixes &rarr;</Link>
            </Button>
          </div>
        </ChartCard>
      </div>
    </div>
  );
}

export default DashboardPage;
