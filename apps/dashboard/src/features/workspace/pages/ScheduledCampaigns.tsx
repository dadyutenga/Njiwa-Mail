import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace, useWsKey } from "../context";
import {
  ConfirmButton,
  inputClass,
  run,
  s,
  Section,
  Table,
  Time,
  type Page,
  type Row,
} from "../ui";

/** Picks a new send time and applies it. */
export function Reschedule({ path, onDone }: { path: string; onDone: () => void }) {
  const phrase = usePhrase();
  const { api } = useWorkspace();
  const [value, setValue] = useState("");
  return (
    <form
      className="flex flex-wrap items-center gap-1"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!value) return;
        await run(
          () => api.send(path, "POST", { send_at: new Date(value).toISOString() }),
          phrase("Send time changed."),
        );
        setValue("");
        onDone();
      }}
    >
      <input
        type="datetime-local"
        aria-label={phrase("New send time")}
        className={inputClass + " w-auto"}
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
      <Button size="sm" variant="outline" type="submit" disabled={!value}>
        {phrase("Reschedule")}
      </Button>
    </form>
  );
}

/** Campaigns and newsletters waiting for their send time (Live). */
export function ScheduledCampaigns() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const key = useWsKey();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: key("campaigns", "scheduled"),
    queryFn: () => api.get<Page>("/campaigns?limit=100"),
  });
  const waiting = (q.data?.items ?? []).filter((c: Row) => c.state === "scheduled");
  if (waiting.length === 0) return null;
  const refresh = () => client.invalidateQueries({ queryKey: key("campaigns", "scheduled") });
  return (
    <Section className="mb-4" title={phrase("Scheduled campaigns and newsletters")}>
      <Table
        rows={waiting}
        rowKey={(r) => s(r.id)}
        columns={[
          {
            key: "name",
            label: phrase("Name"),
            render: (r) => (
              <Link className="font-medium text-primary" to={to(`bulk/${s(r.id)}`)}>
                {s(r.name)}
              </Link>
            ),
          },
          { key: "recipients", label: phrase("Recipients"), render: (r) => s(r.recipients) },
          { key: "send_at", label: phrase("Sends at"), render: (r) => <Time value={r.send_at} /> },
          {
            key: "x",
            label: <span className="sr-only">{phrase("Actions")}</span>,
            render: (r) => (
              <span className="flex flex-wrap items-center gap-2">
                <Reschedule path={`/campaigns/${s(r.id)}/reschedule`} onDone={refresh} />
                <ConfirmButton
                  title={`${phrase("Cancel")} ${s(r.name)}?`}
                  description={phrase("It will not be sent. This cannot be undone.")}
                  confirm="Cancel announcement"
                  onConfirm={async () => {
                    await run(
                      () => api.send(`/campaigns/${s(r.id)}/cancel`, "POST"),
                      phrase("Cancelled."),
                    );
                    refresh();
                  }}
                >
                  {phrase("Cancel")}
                </ConfirmButton>
              </span>
            ),
          },
        ]}
      />
    </Section>
  );
}
