import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { usePhrase } from "@/features/i18n/i18n";
import { admin, type Page } from "@/lib/admin-api";
import { useWorkspace } from "../context";
import { ConfirmButton, errorMessage, run, s, Section, Table, Time, type Row } from "../ui";

type Keys = {
  live_active: number;
  live_revoked: number;
  sandbox_active: number;
  sandbox_revoked: number;
};

/** API keys of a project: active and revoked (or expired), per environment. */
export function KeyCounts({ keys }: { keys?: Keys }) {
  const phrase = usePhrase();
  if (!keys) return null;
  return (
    <span className="block text-xs leading-5">
      <span className="block">
        Live: {keys.live_active} {phrase("active")}, {keys.live_revoked} {phrase("revoked")}
      </span>
      <span className="block">
        Sandbox: {keys.sandbox_active} {phrase("active")}, {keys.sandbox_revoked}{" "}
        {phrase("revoked")}
      </span>
    </span>
  );
}

const removalLabels: [string, string][] = [
  ["api_keys", "API keys"],
  ["senders", "Senders"],
  ["domains", "Domains"],
  ["domain_claims", "Domain claims (released)"],
  ["templates", "Templates"],
  ["contacts", "Contacts"],
  ["messages", "Messages"],
  ["received_messages", "Received messages"],
  ["attachments", "Attachments"],
  ["webhooks", "Webhooks"],
  ["forms", "Forms"],
  ["chats", "Chats"],
];

/**
 * Archive or delete: lists what is affected, shows how many Live messages still
 * wait, and needs the project name typed.
 */
function RemovalDialog({
  name,
  permanent,
  onClose,
}: {
  name: string;
  permanent: boolean;
  onClose: () => void;
}) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const navigate = useNavigate();
  const { project } = useWorkspace();
  const [typed, setTyped] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const path = `/projects/${encodeURIComponent(name)}`;
  const counts = useQuery({
    queryKey: ["projects", "removal", name],
    queryFn: () => admin.get<Row>(`${path}/removal`),
    staleTime: 0,
  });
  const waiting = Number(counts.data?.queued_live ?? 0);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>
          {phrase(permanent ? "Delete permanently" : "Archive project")}: {name}
        </DialogTitle>
        <DialogDescription>
          {phrase(
            permanent
              ? "Everything below is removed for good and its domain claims are released. The audit log keeps its history. This cannot be undone."
              : "The project is hidden and cannot send or receive. You can restore it for 30 days; after that it and everything below are deleted for good.",
          )}
        </DialogDescription>
        {counts.data && (
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm" data-testid="removal-counts">
            {removalLabels.map(([key, label]) => (
              <div key={key} className="flex justify-between gap-2">
                <dt className="text-muted-foreground">{phrase(label)}</dt>
                <dd>{s(counts.data[key])}</dd>
              </div>
            ))}
          </dl>
        )}
        {waiting > 0 && (
          <p role="alert" className="text-sm text-destructive">
            {phrase("Live messages still waiting to be sent")}: {waiting}.{" "}
            {phrase("Let them go out or cancel them first.")}
          </p>
        )}
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError("");
            try {
              if (permanent) await admin.send(path, "DELETE", { confirm: typed });
              else await admin.send(`${path}/archive`, "POST", { confirm: typed });
              await client.invalidateQueries({ queryKey: ["projects"] });
              onClose();
              if (name === project) navigate("/app", { replace: true });
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <label className="grid gap-1.5 text-sm">
            <span>
              {phrase("Type the project name to confirm")}: <code>{name}</code>
            </span>
            <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
          </label>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button
            type="submit"
            variant="destructive"
            disabled={busy || typed !== name || waiting > 0 || !counts.data}
          >
            {phrase(permanent ? "Delete permanently" : "Archive project")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RenameDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const navigate = useNavigate();
  const { project } = useWorkspace();
  const [to, setTo] = useState(name);
  const [error, setError] = useState("");
  const valid = /^[A-Za-z0-9_-]{1,64}$/.test(to) && to !== name;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>
          {phrase("Rename project")}: {name}
        </DialogTitle>
        <DialogDescription>
          {phrase(
            "The name is also the project's address in the dashboard. API keys, domains and mail stay with the project.",
          )}
        </DialogDescription>
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await admin.send(`/projects/${encodeURIComponent(name)}/rename`, "POST", {
                name: to,
              });
              await client.invalidateQueries({ queryKey: ["projects"] });
              onClose();
              if (name === project)
                navigate(`/p/${encodeURIComponent(to)}/projects`, { replace: true });
            } catch (err) {
              setError(errorMessage(err));
            }
          }}
        >
          <label className="grid gap-1.5 text-sm">
            <span>{phrase("New name")}</span>
            <Input value={to} onChange={(e) => setTo(e.target.value)} />
          </label>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button type="submit" disabled={!valid}>
            {phrase("Rename")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Owner actions on one project row. */
export function ProjectActions({ name }: { name: string }) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [dialog, setDialog] = useState<"rename" | "archive" | null>(null);
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setDialog("rename")}>
        {phrase("Rename")}
      </Button>
      <ConfirmButton
        title={`${phrase("Revoke all keys")}: ${name}`}
        description={phrase(
          "Every API key of this project stops working at once, in Live and Sandbox. This cannot be undone.",
        )}
        confirm="Revoke all keys"
        onConfirm={async () => {
          await run(
            () => admin.send(`/projects/${encodeURIComponent(name)}/revoke-keys`, "POST"),
            phrase("All keys revoked."),
          );
          client.invalidateQueries({ queryKey: ["projects"] });
        }}
      >
        {phrase("Revoke all keys")}
      </ConfirmButton>
      <Button size="sm" variant="outline" onClick={() => setDialog("archive")}>
        {phrase("Archive")}
      </Button>
      {dialog === "rename" && <RenameDialog name={name} onClose={() => setDialog(null)} />}
      {dialog === "archive" && (
        <RemovalDialog name={name} permanent={false} onClose={() => setDialog(null)} />
      )}
    </>
  );
}

/** Archived projects, which can be restored or deleted for good. */
export function ArchivedProjects() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [deleting, setDeleting] = useState<string | null>(null);
  const q = useQuery({
    queryKey: ["projects", "archived"],
    queryFn: () => admin.get<Page<Row>>("/projects?archived=true&limit=100"),
  });
  const items = q.data?.items ?? [];
  if (items.length === 0) return null;
  return (
    <Section className="mt-6" title={phrase("Archived projects")}>
      <Table
        rows={items}
        rowKey={(r) => s(r.id)}
        columns={[
          { key: "name", label: phrase("Project"), render: (r) => s(r.name) },
          {
            key: "purge_after",
            label: phrase("Deleted for good on"),
            render: (r) => <Time value={r.purge_after} />,
          },
          {
            key: "x",
            label: <span className="sr-only">{phrase("Actions")}</span>,
            render: (r) =>
              r.role === "owner" ? (
                <span className="flex gap-1">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={async () => {
                      await run(
                        () =>
                          admin.send(`/projects/${encodeURIComponent(s(r.name))}/restore`, "POST"),
                        phrase("Project restored."),
                      );
                      client.invalidateQueries({ queryKey: ["projects"] });
                    }}
                  >
                    {phrase("Restore")}
                  </Button>
                  <Button size="sm" variant="destructive" onClick={() => setDeleting(s(r.name))}>
                    {phrase("Delete permanently")}
                  </Button>
                </span>
              ) : null,
          },
        ]}
      />
      {deleting && <RemovalDialog name={deleting} permanent onClose={() => setDeleting(null)} />}
    </Section>
  );
}
