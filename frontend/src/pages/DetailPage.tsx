import { Link, useParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ActivityFeed } from "@/components/shared/ActivityFeed";
import { CopyButton } from "@/components/shared/CopyButton";
import { useMessageQuery } from "@/features/console/mailApi";
import { formatDate } from "@/lib/utils";
import type { ActivityItem } from "@/types/dashboard";

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="text-sm break-all">{value}</p>
    </div>
  );
}

export function DetailPage() {
  const { id } = useParams<{ id: string }>();
  const { data: message, isLoading, isError, error } = useMessageQuery(id);

  if (isLoading) {
    return (
      <div className="space-y-6 animate-pulse">
        <div className="h-8 w-48 bg-muted rounded" />
        <div className="h-32 w-full bg-muted rounded-xl" />
      </div>
    );
  }

  if (isError || !message) {
    return (
      <div className="rounded-xl border border-dashed p-12 text-center">
        <h2 className="text-lg font-bold">Message not available</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {isError ? error.message : "That id is not in the queue."}
        </p>
        <Button className="mt-4" asChild>
          <Link to="/messages">Back to messages</Link>
        </Button>
      </div>
    );
  }

  const events: ActivityItem[] = (message.events ?? []).map((event, index) => ({
    id: `${message.id}-${index}`,
    user: { name: event.type },
    action: event.reason || "recorded",
    target: message.template,
    timestamp: event.at,
    type: event.type === "failed" || event.type === "expired" ? "delete" : "update",
  }));

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-2">
          <Button variant="ghost" size="sm" asChild className="-ml-2 h-8 px-2">
            <Link to="/messages" aria-label="Back to messages">
              <ArrowLeft className="h-4 w-4" />
              Messages
            </Link>
          </Button>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-bold tracking-tight font-mono">{message.id}</h1>
            <CopyButton value={message.id} />
            <Badge className="capitalize">{message.state}</Badge>
          </div>
          <p className="text-sm text-muted-foreground max-w-2xl">
            {message.state === "submitted"
              ? "The SMTP server accepted this message. That is not a delivery receipt."
              : message.state === "captured"
                ? "Sandbox capture. The raw message stays on the server and is shown with the CLI, not here."
                : "Metadata only. Recipients and template variables are not returned to the browser."}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Route</CardTitle>
            <CardDescription>Project environment and template</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4">
            <Field label="Project" value={`${message.project} / ${message.mode}`} />
            <Field label="Template" value={`${message.template} v${message.template_version}`} />
            <Field label="Attempts" value={String(message.attempts)} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Times</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4">
            <Field label="Accepted" value={formatDate(message.created_at, "MMM d, yyyy HH:mm:ss")} />
            <Field label="Updated" value={formatDate(message.updated_at, "MMM d, yyyy HH:mm:ss")} />
            <Field label="Expires" value={formatDate(message.expires_at, "MMM d, yyyy HH:mm:ss")} />
            <Field
              label="Submitted"
              value={message.submitted_at ? formatDate(message.submitted_at, "MMM d, yyyy HH:mm:ss") : "—"}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Correlation</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4">
            <Field label="Idempotency key" value={message.idempotency_key} />
            <Field label="Request id" value={message.metadata?.request_id || "—"} />
            <Field label="SMTP reply" value={message.smtp_reply || "—"} />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Events</CardTitle>
          <CardDescription>Safe reason codes only</CardDescription>
        </CardHeader>
        <CardContent>
          <ActivityFeed items={events} emptyMessage="No events recorded." />
        </CardContent>
      </Card>
    </div>
  );
}

export default DetailPage;
