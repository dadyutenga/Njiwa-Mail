import { useNavigate } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { DataTable, type ColumnDef } from "@/components/shared/DataTable";
import { useMessagesQuery } from "@/features/console/mailApi";
import { formatDate } from "@/lib/utils";
import type { MailMessage } from "@/types/mail";

function stateBadge(state: string) {
  if (state === "submitted" || state === "captured") {
    return <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30">{state}</Badge>;
  }
  if (state === "failed" || state === "expired" || state === "unknown") {
    return <Badge className="bg-rose-500/15 text-rose-600 dark:text-rose-400 border-rose-500/30">{state}</Badge>;
  }
  if (state === "queued" || state === "retry_wait" || state === "submitting") {
    return <Badge className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30">{state}</Badge>;
  }
  return <Badge variant="outline">{state}</Badge>;
}

export function ListPage() {
  const navigate = useNavigate();
  const { data: messages = [], isLoading, isError, error } = useMessagesQuery();

  const columns: ColumnDef<MailMessage>[] = [
    {
      key: "id",
      header: "Message",
      sortable: true,
      render: (item) => <span className="font-mono text-xs">{item.id.slice(0, 8)}</span>,
    },
    {
      key: "state",
      header: "State",
      sortable: true,
      render: (item) => stateBadge(item.state),
    },
    {
      key: "template",
      header: "Template",
      sortable: true,
      render: (item) => (
        <span>
          {item.template} <span className="text-muted-foreground">v{item.template_version}</span>
        </span>
      ),
    },
    {
      key: "project",
      header: "Project",
      sortable: true,
      render: (item) => (
        <span>
          {item.project}
          <span className="text-muted-foreground"> / {item.mode}</span>
        </span>
      ),
    },
    {
      key: "attempts",
      header: "Attempts",
      sortable: true,
    },
    {
      key: "created_at",
      header: "Accepted",
      sortable: true,
      render: (item) => formatDate(item.created_at, "MMM d, yyyy HH:mm"),
    },
  ];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Messages</h1>
        <p className="text-sm text-muted-foreground">
          Durable queue metadata. Recipients, variables, and message bodies stay on the server.
        </p>
      </div>
      {isError && (
        <div className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          {error.message}
        </div>
      )}
      <DataTable
        data={messages}
        columns={columns}
        isLoading={isLoading}
        searchPlaceholder="Search by message id..."
        searchKey="id"
        onRowClick={(item) => navigate(`/messages/${item.id}`)}
        onView={(item) => navigate(`/messages/${item.id}`)}
        exportFileName="ditronics-messages.csv"
        pageSize={20}
      />
    </div>
  );
}

export default ListPage;
