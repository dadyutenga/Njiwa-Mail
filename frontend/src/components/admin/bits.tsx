import { useState, type FormEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useAuth } from "@/contexts/AuthContext";
import { atLeast, type MailRole } from "@/lib/roles";

export const PAGE_SIZE = 25;

export type Row = Record<string, unknown>;

export interface PageResult {
  items: Row[];
  has_more: boolean;
  limit: number;
  offset: number;
}

export async function getJSON<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
  const response = await apiClient.get<T>(path, { params });
  return response.data;
}

export async function send(method: "post" | "patch" | "put" | "delete", path: string, body?: unknown) {
  const response = await apiClient.request({ method, url: path, data: body });
  return response.data as unknown;
}

export function useList(path: string, params: Record<string, string | number | undefined>) {
  return useQuery({
    queryKey: ["admin", path, params],
    queryFn: () => getJSON<PageResult>(path, { limit: PAGE_SIZE, ...params }),
  });
}

export function useCan(min: MailRole): boolean {
  const { user } = useAuth();
  return atLeast(user?.role, min);
}

export function errText(error: unknown): string {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  return "The request failed.";
}

export function show(value: unknown): string {
  if (value == null || value === "") return "—";
  if (typeof value === "boolean") return value ? "yes" : "no";
  if (Array.isArray(value)) return value.map((item) => show(item)).join(", ");
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

export function lines(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

const ZONE = "Africa/Dar_es_Salaam";

export function When({ value }: { value: unknown }) {
  if (typeof value !== "string" || value === "") return <span>—</span>;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return <span>{value}</span>;
  const local = new Intl.DateTimeFormat("en-GB", {
    timeZone: ZONE,
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
  return <span title={date.toISOString()}>{local}</span>;
}

const STATE_VARIANT: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  submitted: "default",
  captured: "default",
  active: "default",
  queued: "secondary",
  submitting: "secondary",
  retry_wait: "outline",
  unknown: "outline",
  soft: "secondary",
  policy: "outline",
  failed: "destructive",
  expired: "destructive",
  hard: "destructive",
  revoked: "destructive",
  dead: "destructive",
};

export function StateBadge({ value }: { value: unknown }) {
  const label = typeof value === "string" && value ? value : "—";
  return <Badge variant={STATE_VARIANT[label] ?? "secondary"}>{label}</Badge>;
}

export function PageTitle({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
        {description && <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{description}</p>}
      </div>
      {children}
    </div>
  );
}

export function Banner({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <div className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
      {errText(error)}
    </div>
  );
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid min-w-[10rem] gap-1 text-sm">
      <span className="font-medium">{label}</span>
      {children}
    </label>
  );
}

export const controlClass =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

export function ServerTable({
  columns,
  rows,
  loading,
  error,
  empty,
  offset,
  hasMore,
  onPage,
}: {
  columns: { header: string; cell: (row: Row) => ReactNode }[];
  rows: Row[] | undefined;
  loading: boolean;
  error: unknown;
  empty: string;
  offset: number;
  hasMore?: boolean;
  onPage: (offset: number) => void;
}) {
  if (loading) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (error) return <Banner error={error} />;
  if (!rows || rows.length === 0) return <EmptyState title={empty} className="min-h-[180px]" />;
  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((column) => (
                <TableHead key={column.header}>{column.header}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row, index) => (
              <TableRow key={String(row.id ?? row.prefix ?? index)}>
                {columns.map((column) => (
                  <TableCell key={column.header}>{column.cell(row)}</TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button type="button" variant="outline" size="sm" disabled={offset === 0} onClick={() => onPage(Math.max(0, offset - PAGE_SIZE))}>
          Previous
        </Button>
        <Button type="button" variant="outline" size="sm" disabled={!hasMore} onClick={() => onPage(offset + PAGE_SIZE)}>
          Next
        </Button>
      </div>
    </div>
  );
}

export function Modal({
  open,
  title,
  onClose,
  children,
  footer,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {children}
        {footer && <DialogFooter>{footer}</DialogFooter>}
      </DialogContent>
    </Dialog>
  );
}

export function useDraft<T>(initial: T) {
  const [draft, setDraft] = useState(initial);
  const [applied, setApplied] = useState(initial);
  const [offset, setOffset] = useState(0);
  const apply = (event?: FormEvent) => {
    event?.preventDefault();
    setApplied(draft);
    setOffset(0);
  };
  return { draft, setDraft, applied, offset, setOffset, apply };
}
