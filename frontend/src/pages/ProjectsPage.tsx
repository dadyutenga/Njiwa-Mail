import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useKeysQuery, useProjectsQuery } from "@/features/console/mailApi";

export function ProjectsPage() {
  const projects = useProjectsQuery();
  const keys = useKeysQuery();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Projects</h1>
        <p className="text-sm text-muted-foreground max-w-2xl">
          Keys shown here are prefixes only. The secret is printed once by <span className="font-mono">key create</span> and
          is never stored or sent to this console.
        </p>
      </div>

      {(projects.isError || keys.isError) && (
        <div className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          {projects.error?.message || keys.error?.message}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {(projects.data ?? []).map((project) => (
          <Card key={project.name}>
            <CardHeader>
              <div className="flex items-center justify-between gap-2">
                <CardTitle>{project.name}</CardTitle>
                <Badge variant={project.enabled ? "default" : "secondary"}>
                  {project.enabled ? "enabled" : "disabled"}
                </Badge>
              </div>
              <CardDescription>
                {project.send_per_minute}/min · {project.send_per_day}/day · recipient {project.recipient_per_minute}/min, {project.recipient_per_hour}/hour
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {project.environments.map((env) => (
                <div key={env.mode} className="rounded-lg border p-3 text-sm">
                  <div className="flex items-center justify-between">
                    <span className="font-medium capitalize">{env.mode}</span>
                    <span className="text-xs text-muted-foreground">{env.enabled ? "enabled" : "disabled"}</span>
                  </div>
                  <p className="mt-1 text-muted-foreground">
                    {env.sender_count} sender{env.sender_count === 1 ? "" : "s"} · {env.origin_count} origin{env.origin_count === 1 ? "" : "s"} ·{" "}
                    {env.restrict_recipients ? `${env.allowlist_count} allowlist pattern${env.allowlist_count === 1 ? "" : "s"}` : "recipients unrestricted"}
                  </p>
                </div>
              ))}
            </CardContent>
          </Card>
        ))}
        {projects.data && projects.data.length === 0 && (
          <p className="text-sm text-muted-foreground">No projects yet. Create one with the CLI.</p>
        )}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">API key prefixes</CardTitle>
          <CardDescription>Revoke and rotate from the CLI. This list cannot reveal a secret.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {(keys.data ?? []).map((key) => (
            <div key={key.prefix} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
              <div>
                <p className="font-mono text-xs">{key.prefix}</p>
                <p className="text-muted-foreground">
                  {key.project} / {key.mode}
                  {key.label ? ` · ${key.label}` : ""} · {key.scopes.join(", ")}
                </p>
              </div>
              <Badge variant="outline">{key.status}</Badge>
            </div>
          ))}
          {keys.data && keys.data.length === 0 && (
            <p className="text-sm text-muted-foreground">No keys issued.</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export default ProjectsPage;
