import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { admin, adminRequest, AdminError, type Page } from "@/lib/admin-api";

export type Env = "sandbox" | "live";

export type WorkspaceInfo = {
  project: string;
  mode: Env;
  project_enabled: boolean;
  environment_enabled: boolean;
  restrict_recipients: boolean;
  recipient_allowlist: string[] | null;
  senders: number;
  verified_senders: number;
  default_sender: string | null;
  domains: number;
  mailboxes: number;
  unread: number;
  inbound_domain: string;
  api_host: string;
  bulk_max_per_campaign: number;
  bulk_max_per_day: number;
  temp_inbox_limit: number;
};

export type ProjectRow = {
  id: string;
  name: string;
  enabled: boolean;
  send_per_minute: number;
  send_per_day: number;
  recipient_per_minute: number;
  recipient_per_hour: number;
  created_at: string;
};

const LAST_KEY = "njiwa.workspace";

export function rememberWorkspace(project: string, env: Env) {
  try {
    localStorage.setItem(LAST_KEY, JSON.stringify({ project, env }));
  } catch {
    /* storage unavailable: the URL still carries the workspace */
  }
}

export function lastWorkspace(): { project: string; env: Env } | null {
  try {
    const v = JSON.parse(localStorage.getItem(LAST_KEY) || "null");
    if (v && typeof v.project === "string" && (v.env === "sandbox" || v.env === "live")) return v;
  } catch {
    /* ignore */
  }
  return null;
}

/** The scoped API: every call goes to /admin/v1/w/{project}/{env}/... */
export function scopedApi(project: string, env: Env) {
  const base = `/w/${encodeURIComponent(project)}/${env}`;
  return {
    base,
    get: <T,>(path: string) => admin.get<T>(base + path),
    send: <T,>(path: string, method: "POST" | "PATCH" | "PUT" | "DELETE", data?: unknown) =>
      admin.send<T>(base + path, method, data),
    /** POST with an Idempotency-Key header (compose: a double click cannot send twice). */
    sendWithKey: <T,>(path: string, key: string, data: unknown) =>
      adminRequest<T>(base + path, {
        method: "POST",
        body: JSON.stringify(data),
        headers: { "Idempotency-Key": key },
      }),
  };
}

type Ctx = {
  project: string;
  env: Env;
  info: WorkspaceInfo | undefined;
  api: ReturnType<typeof scopedApi>;
  /** Path inside this workspace, e.g. to("inbox") → /p/sacas/sandbox/inbox */
  to: (path?: string) => string;
};

const WorkspaceContext = createContext<Ctx | null>(null);

export function WorkspaceProvider({
  project,
  env,
  children,
}: {
  project: string;
  env: Env;
  children: ReactNode;
}) {
  const api = useMemo(() => scopedApi(project, env), [project, env]);
  const info = useQuery({
    queryKey: ["ws", project, env, "context"],
    queryFn: () => api.get<WorkspaceInfo>("/context"),
    staleTime: 30_000,
    retry: (n, e) => !(e instanceof AdminError && e.status === 404) && n < 1,
  });
  const value = useMemo<Ctx>(
    () => ({
      project,
      env,
      info: info.data,
      api,
      to: (path = "") =>
        `/p/${encodeURIComponent(project)}/${env}${path ? "/" + path.replace(/^\//, "") : ""}`,
    }),
    [project, env, info.data, api],
  );
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace() {
  const v = useContext(WorkspaceContext);
  if (!v) throw new Error("WorkspaceProvider is missing");
  return v;
}

/** Query key scoped to the current workspace, so switching reloads data. */
export function useWsKey() {
  const { project, env } = useWorkspace();
  return (...parts: unknown[]) => ["ws", project, env, ...parts];
}

export async function allProjects(): Promise<ProjectRow[]> {
  const out: ProjectRow[] = [];
  for (let offset = 0; ; offset += 100) {
    const page = await admin.get<Page<ProjectRow>>(`/projects?limit=100&offset=${offset}`);
    out.push(...page.items);
    if (!page.has_more) return out;
  }
}

export function useProjects() {
  return useQuery({ queryKey: ["projects", "all"], queryFn: allProjects, staleTime: 30_000 });
}
