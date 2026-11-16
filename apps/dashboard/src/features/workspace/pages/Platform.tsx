import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { DataState, n, PageHeader, s, Section, StateBadge, Table, Time, type Row } from "../ui";

const jobState: Record<string, string> = {
  ok: "active",
  never: "pending",
  failing: "failed",
  stale: "failed",
};

type Snapshot = {
  at: string;
  version: string;
  stats: {
    queue_depth: number;
    oldest_queued_age_seconds: number;
    sent_today: number;
    open_alerts: number;
    webhook_backlog: number;
    inbound_spool_pending: number;
    workers: { role: string; instance: string; healthy: boolean }[];
  };
  jobs: { total: number; problems: Record<string, number> };
};

/** The installation operator's view of the platform: live health, scheduled jobs and updates. */
export function Platform({ embedded = false }: { embedded?: boolean }) {
  const phrase = usePhrase();
  const [snap, setSnap] = useState<Snapshot | null>(null);
  const [alerts, setAlerts] = useState<Row[]>([]);
  const [live, setLive] = useState(false);
  const jobs = useQuery({
    queryKey: ["platform-jobs"],
    queryFn: () => admin.get<{ items: Row[] }>("/jobs"),
    refetchInterval: 30000,
  });
  const update = useQuery({
    queryKey: ["platform-update"],
    queryFn: () => admin.get<Row>("/platform/update-check"),
  });
  // Live view: the browser reconnects by itself when the stream ends.
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const es = new EventSource("/admin/v1/platform/stream", { withCredentials: true });
    es.onopen = () => setLive(true);
    es.onerror = () => setLive(false);
    es.addEventListener("snapshot", (e) => setSnap(JSON.parse((e as MessageEvent).data)));
    es.addEventListener("alert", (e) =>
      setAlerts((a) => [JSON.parse((e as MessageEvent).data) as Row, ...a].slice(0, 20)),
    );
    return () => es.close();
  }, []);
  const problems = snap ? Object.values(snap.jobs.problems).reduce((a, b) => a + b, 0) : 0;
  const tiles: [string, string | number][] = snap
    ? [
        [phrase("Queue"), snap.stats.queue_depth],
        [phrase("Oldest queued (s)"), Math.round(snap.stats.oldest_queued_age_seconds)],
        [phrase("Sent today"), snap.stats.sent_today],
        [phrase("Open alerts"), snap.stats.open_alerts],
        [phrase("Webhook backlog"), snap.stats.webhook_backlog],
        [phrase("Inbound waiting"), snap.stats.inbound_spool_pending],
      ]
    : [];
  return (
    <>
      {embedded ? (
        <div className="mt-8 mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">{phrase("Platform")}</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {phrase(
                "Live health of this installation, what the scheduler's jobs last did, and whether a newer release exists.",
              )}
            </p>
          </div>
          <StateBadge
            state={live ? "active" : "cancelled"}
            label={phrase(live ? "Live" : "Reconnecting")}
          />
        </div>
      ) : (
        <PageHeader
          title={phrase("Platform")}
          description={phrase(
            "Live health of this installation, what the scheduler's jobs last did, and whether a newer release exists.",
          )}
          action={
            <StateBadge
              state={live ? "active" : "cancelled"}
              label={phrase(live ? "Live" : "Reconnecting")}
            />
          }
        />
      )}
      <Section title={phrase("Now")} className="mb-4">
        {snap ? (
          <>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
              {tiles.map(([label, value]) => (
                <div key={label} className="rounded-lg border p-3">
                  <div className="text-xs text-muted-foreground">{label}</div>
                  <div className="text-xl font-semibold">{value}</div>
                </div>
              ))}
            </div>
            <p className="mt-3 text-sm">
              {phrase("Version")}: <code>{snap.version}</code> · {phrase("Workers")}:{" "}
              {snap.stats.workers.length === 0
                ? phrase("none seen")
                : snap.stats.workers
                    .map((w) => `${w.role}${w.healthy ? "" : ` (${phrase("silent")})`}`)
                    .join(", ")}{" "}
              · {phrase("Jobs with a problem")}: {problems}
            </p>
          </>
        ) : (
          <p role="status">{phrase("Loading…")}</p>
        )}
      </Section>
      {alerts.length > 0 && (
        <Section title={phrase("New alerts")} className="mb-4">
          <ul className="space-y-1 text-sm">
            {alerts.map((a) => (
              <li key={s(a.id)}>
                <code>{s(a.kind)}</code> {s(a.summary)}
              </li>
            ))}
          </ul>
        </Section>
      )}
      <Section title={phrase("Scheduled jobs")} className="mb-4">
        <DataState q={jobs}>
          {(d) => (
            <Table
              rows={d.items}
              rowKey={(r) => s(r.name)}
              columns={[
                { key: "name", label: phrase("Job"), render: (r) => <code>{s(r.name)}</code> },
                {
                  key: "status",
                  label: phrase("Status"),
                  render: (r) => (
                    <StateBadge
                      state={jobState[s(r.status)] ?? "cancelled"}
                      label={phrase(s(r.status))}
                    />
                  ),
                },
                {
                  key: "interval_seconds",
                  label: phrase("Every (s)"),
                },
                {
                  key: "last_finished_at",
                  label: phrase("Last run"),
                  render: (r) =>
                    r.last_finished_at ? <Time value={r.last_finished_at} relative /> : "-",
                },
                {
                  key: "last_duration_ms",
                  label: phrase("Took (ms)"),
                  render: (r) => (r.last_finished_at ? n(r.last_duration_ms) : "-"),
                },
                { key: "runs", label: phrase("Runs") },
                { key: "failures", label: phrase("Failures") },
                {
                  key: "last_error",
                  label: phrase("Last error"),
                  render: (r) => <span className="text-destructive">{s(r.last_error)}</span>,
                },
              ]}
            />
          )}
        </DataState>
      </Section>
      <Section title={phrase("Updates")}>
        <DataState q={update}>
          {(u) =>
            !u.enabled ? (
              <p className="text-sm text-muted-foreground">
                {phrase(
                  "The update check is off. Set UPDATE_CHECK_URL to an https release feed to switch it on; nothing is sent about this installation.",
                )}
              </p>
            ) : (
              <div className="space-y-2 text-sm">
                <p>
                  {phrase("Running")}: <code>{s(u.current)}</code> · {phrase("Latest")}:{" "}
                  <code>{s(u.latest) || "-"}</code>
                  {!!u.update_available && (
                    <>
                      {" "}
                      <StateBadge state="pending" label={phrase("Update available")} />
                    </>
                  )}
                </p>
                {!!u.error && <p className="text-destructive">{s(u.error)}</p>}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => update.refetch()}
                  disabled={update.isFetching}
                >
                  {phrase("Check again")}
                </Button>
              </div>
            )
          }
        </DataState>
      </Section>
    </>
  );
}
