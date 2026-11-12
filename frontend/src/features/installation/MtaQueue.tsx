import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { admin, AdminError } from "@/lib/admin-api";
import { useWorkspace } from "@/features/workspace/context";
import {
  ConfirmButton,
  DataState,
  Empty,
  inputClass,
  PageHeader,
  run,
  s,
  Section,
  Table,
  Time,
  type Row,
} from "@/features/workspace/ui";

// E10: the mail server's queue (managed mode). The API never touches the host:
// a small agent on the mail host runs postqueue/postsuper. Addresses are masked.

type Status = {
  configured: boolean;
  reachable?: boolean;
  status?: {
    queue: Record<string, number>;
    total: number;
    postfix_active: boolean;
    signer_active: boolean;
  };
};
type Queue = { items: Row[] };

const statusKey = ["mta", "status"] as const;
const queueKey = ["mta", "queue"] as const;

function DeleteDialog({
  id,
  onClose,
  onDone,
}: {
  id: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const phrase = usePhrase();
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>{phrase("Delete this message from the mail queue?")}</DialogTitle>
        <DialogDescription>
          {phrase(
            "It will never be delivered. Njiwa-Mail keeps the message record and notes the deletion on its timeline. This cannot be undone.",
          )}
        </DialogDescription>
        <label className="block space-y-1 text-sm font-medium">
          {phrase("Type the queue ID to confirm")} <code>{id}</code>
          <input
            className={inputClass}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
          />
        </label>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>
            {phrase("Cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={busy || typed.trim().toLowerCase() !== id.toLowerCase()}
            onClick={async () => {
              setBusy(true);
              const ok = await run(
                () => admin.send(`/mta/queue/${id}/delete`, "POST", { confirm: typed.trim() }),
                phrase("Deleted from the mail queue."),
              );
              setBusy(false);
              if (ok !== undefined) onDone();
            }}
          >
            {phrase("Delete from queue")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function MtaQueue() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const { to } = useWorkspace();
  const [deleting, setDeleting] = useState("");
  const status = useQuery({
    queryKey: statusKey,
    queryFn: () => admin.get<Status>("/mta/status"),
    refetchInterval: 15_000,
  });
  const configured = status.data?.configured !== false;
  const queue = useQuery({
    queryKey: queueKey,
    queryFn: () => admin.get<Queue>("/mta/queue"),
    enabled: status.data?.configured === true,
    refetchInterval: 15_000,
    retry: (n, e) => !(e instanceof AdminError) && n < 2,
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: queueKey });
    client.invalidateQueries({ queryKey: statusKey });
  };
  const act = async (id: string, action: string, done: string) => {
    await run(() => admin.send(`/mta/queue/${id}/${action}`, "POST", {}), done);
    refresh();
  };
  const st = status.data?.status;
  return (
    <>
      <PageHeader
        title={phrase("Mail queue")}
        description={phrase(
          "Messages your mail server has accepted but not yet handed to the recipient's server. Addresses are hidden here.",
        )}
        action={
          configured && status.data?.reachable ? (
            <ConfirmButton
              size="default"
              destructive={false}
              title={phrase("Retry every deferred message now?")}
              description={phrase("The mail server tries all deferred messages immediately.")}
              confirm="Retry now"
              onConfirm={async () => {
                await run(() => admin.send("/mta/flush", "POST", {}), phrase("Retrying now."));
                refresh();
              }}
            >
              {phrase("Retry all now")}
            </ConfirmButton>
          ) : undefined
        }
      />
      {status.data && !status.data.configured && (
        <Empty
          title={phrase("The queue agent is not set up")}
          description={phrase(
            "Run `njiwa-mail mta-agent` on the mail host and set MTA_AGENT_SOCKET and MTA_AGENT_TOKEN for the API (see Operations: Mail queue controls).",
          )}
        />
      )}
      {status.data?.configured && status.data.reachable === false && (
        <p role="alert" className="rounded-lg border p-3 text-sm">
          {phrase("The queue agent cannot be reached. Check that njiwa-mail-mta-agent is running.")}
        </p>
      )}
      {st && (
        <Section>
          <dl className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
            <div>
              <dt className="text-muted-foreground">{phrase("In queue")}</dt>
              <dd className="font-semibold">{st.total}</dd>
            </div>
            {Object.entries(st.queue).map(([name, n]) => (
              <div key={name}>
                <dt className="text-muted-foreground">{name}</dt>
                <dd className="font-semibold">{n}</dd>
              </div>
            ))}
            <div>
              <dt className="text-muted-foreground">Postfix</dt>
              <dd className="font-semibold">
                {st.postfix_active ? phrase("running") : phrase("stopped")}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{phrase("DKIM signer")}</dt>
              <dd className="font-semibold">
                {st.signer_active ? phrase("running") : phrase("stopped")}
              </dd>
            </div>
          </dl>
        </Section>
      )}
      {configured && status.data?.reachable && (
        <DataState q={queue}>
          {(d) =>
            d.items.length === 0 ? (
              <Empty
                title={phrase("The queue is empty")}
                description={phrase("Everything the mail server accepted has been handed on.")}
              />
            ) : (
              <Table
                rows={d.items}
                rowKey={(r) => s(r.queue_id)}
                columns={[
                  {
                    key: "queue_id",
                    label: phrase("Queue ID"),
                    render: (r) => <code>{s(r.queue_id)}</code>,
                  },
                  { key: "queue", label: phrase("Queue") },
                  {
                    key: "arrived_at",
                    label: phrase("Waiting since"),
                    render: (r) => <Time value={r.arrived_at} relative />,
                  },
                  {
                    key: "recipients",
                    label: phrase("Recipients and reason"),
                    render: (r) => (
                      <ul className="max-w-md space-y-1 text-xs">
                        {((r.recipients as Row[]) ?? []).map((x, i) => (
                          <li key={i}>
                            {s(x.address)}
                            {x.delay_reason ? ` — ${s(x.delay_reason)}` : ""}
                          </li>
                        ))}
                      </ul>
                    ),
                  },
                  {
                    key: "message",
                    label: phrase("Message"),
                    render: (r) => {
                      const m = r.message as Row | undefined;
                      return m ? (
                        <Link className="text-primary" to={to(`messages/${s(m.message_id)}`)}>
                          {s(m.template) || s(m.message_id).slice(0, 8)}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      );
                    },
                  },
                  {
                    key: "x",
                    label: <span className="sr-only">{phrase("Actions")}</span>,
                    render: (r) => {
                      const id = s(r.queue_id);
                      const held = r.queue === "hold";
                      return (
                        <span className="flex flex-wrap gap-1">
                          {held ? (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => act(id, "release", phrase("Released."))}
                            >
                              {phrase("Release")}
                            </Button>
                          ) : (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => act(id, "hold", phrase("Held."))}
                            >
                              {phrase("Hold")}
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => act(id, "requeue", phrase("Re-queued."))}
                          >
                            {phrase("Requeue")}
                          </Button>
                          <Button size="sm" variant="destructive" onClick={() => setDeleting(id)}>
                            {phrase("Delete")}
                          </Button>
                        </span>
                      );
                    },
                  },
                ]}
              />
            )
          }
        </DataState>
      )}
      {deleting && (
        <DeleteDialog
          id={deleting}
          onClose={() => setDeleting("")}
          onDone={() => {
            setDeleting("");
            refresh();
          }}
        />
      )}
    </>
  );
}
