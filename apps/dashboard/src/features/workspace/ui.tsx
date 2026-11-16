import { cloneElement, isValidElement, useId, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { AlertTriangle, Check, Copy, Inbox as InboxIcon, RefreshCw } from "lucide-react";
import type { UseQueryResult } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { usePhrase } from "@/features/i18n/i18n";
import { AdminError } from "@/lib/admin-api";
import { cn } from "@/lib/utils";

export type Row = Record<string, unknown>;
export type Page<T = Row> = { items: T[]; has_more: boolean; limit: number; offset: number };

export const s = (v: unknown) => (v === null || v === undefined ? "" : String(v));
export const n = (v: unknown) => (typeof v === "number" ? v : Number(v) || 0);

export function errorMessage(e: unknown, fallback = "Request failed") {
  if (e instanceof AdminError) return e.message;
  if (e instanceof Error) return e.message;
  return fallback;
}

/** Runs an operation with a success or error toast; returns the result or undefined. */
export async function run<T>(fn: () => Promise<T>, success: string): Promise<T | undefined> {
  try {
    const out = await fn();
    if (success) toast.success(success);
    return out;
  } catch (e) {
    toast.error(errorMessage(e));
    return undefined;
  }
}

export function PageHeader({
  title,
  description,
  action,
  back,
}: {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  back?: { to: string; label: string };
}) {
  return (
    <div className="mb-6 space-y-2">
      {back && (
        <Link to={back.to} className="text-sm text-primary hover:underline">
          ← {back.label}
        </Link>
      )}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="font-display text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
          {description && (
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{description}</p>
          )}
        </div>
        {action && <div className="flex shrink-0 flex-wrap gap-2">{action}</div>}
      </div>
    </div>
  );
}

export function Section({
  title,
  action,
  children,
  className,
}: {
  title?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("rounded-xl border bg-card p-4 sm:p-5", className)}>
      {(title || action) && (
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          {title && <h2 className="font-semibold">{title}</h2>}
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

export function LoadingRows({ rows = 5 }: { rows?: number }) {
  return (
    <div role="status" aria-label="Loading" className="space-y-2 p-2">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  );
}

export function ErrorState({ error, retry }: { error: unknown; retry?: () => void }) {
  const phrase = usePhrase();
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/40 p-4 text-sm"
    >
      <AlertTriangle className="text-destructive" size={18} aria-hidden />
      <span className="flex-1">{errorMessage(error)}</span>
      {retry && (
        <Button size="sm" variant="outline" onClick={retry}>
          <RefreshCw size={14} /> {phrase("Retry")}
        </Button>
      )}
    </div>
  );
}

export function Empty({
  title,
  description,
  action,
  icon,
}: {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  icon?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
      <div className="text-muted-foreground">{icon ?? <InboxIcon size={28} aria-hidden />}</div>
      <p className="font-semibold">{title}</p>
      {description && <p className="max-w-md text-sm text-muted-foreground">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/** Loading skeleton, error with retry, empty state, or the content. */
export function DataState<T>({
  q,
  empty,
  isEmpty,
  children,
  rows,
}: {
  q: UseQueryResult<T>;
  empty?: ReactNode;
  isEmpty?: (d: T) => boolean;
  children: (d: T) => ReactNode;
  rows?: number;
}) {
  if (q.isPending) return <LoadingRows rows={rows} />;
  if (q.error) return <ErrorState error={q.error} retry={() => q.refetch()} />;
  const d = q.data as T;
  if (empty && isEmpty?.(d)) return <>{empty}</>;
  return <>{children(d)}</>;
}

/** A button that asks for confirmation, naming what will be lost. */
export function ConfirmButton({
  title,
  description,
  confirm = "Confirm",
  onConfirm,
  children,
  variant = "outline",
  size = "sm",
  destructive = true,
  disabled,
}: {
  title: string;
  description: ReactNode;
  confirm?: string;
  onConfirm: () => Promise<unknown> | void;
  children: ReactNode;
  variant?: "outline" | "ghost" | "destructive" | "default" | "secondary" | "link";
  size?: "sm" | "default" | "icon";
  destructive?: boolean;
  disabled?: boolean;
}) {
  const phrase = usePhrase();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <>
      <Button variant={variant} size={size} disabled={disabled} onClick={() => setOpen(true)}>
        {children}
      </Button>
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{title}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div>{description}</div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{phrase("Cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={busy}
              className={
                destructive
                  ? "bg-destructive text-destructive-foreground hover:bg-destructive/90"
                  : ""
              }
              onClick={async (e) => {
                e.preventDefault();
                setBusy(true);
                try {
                  await onConfirm();
                } finally {
                  setBusy(false);
                  setOpen(false);
                }
              }}
            >
              {phrase(confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      aria-label={label ? `Copy ${label}` : "Copy"}
      onClick={() =>
        navigator.clipboard.writeText(text).then(
          () => {
            setDone(true);
            setTimeout(() => setDone(false), 1500);
          },
          () => toast.error("Could not copy"),
        )
      }
    >
      {done ? <Check size={12} /> : <Copy size={12} />}
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** Local time, with the UTC value on hover. */
export function Time({ value, relative }: { value: unknown; relative?: boolean }) {
  if (!value) return <span className="text-muted-foreground">—</span>;
  const d = new Date(String(value));
  if (Number.isNaN(d.getTime())) return <span>{String(value)}</span>;
  let label = d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  if (relative) {
    const mins = Math.round((Date.now() - d.getTime()) / 60000);
    if (Math.abs(mins) < 60) label = mins >= 0 ? `${mins} min ago` : `in ${-mins} min`;
    else if (Math.abs(mins) < 1440) label = d.toLocaleTimeString(undefined, { timeStyle: "short" });
  }
  return (
    <time
      dateTime={d.toISOString()}
      title={d
        .toISOString()
        .replace("T", " ")
        .replace(/\.\d+Z$/, " UTC")}
    >
      {label}
    </time>
  );
}

const tone: Record<string, string> = {
  submitted: "bg-success/15 text-success",
  captured: "bg-primary/15 text-primary",
  queued: "bg-muted text-foreground",
  submitting: "bg-muted text-foreground",
  retry_wait: "bg-warning/15 text-warning",
  scheduled: "bg-accent/15 text-accent-foreground dark:text-accent",
  failed: "bg-destructive/15 text-destructive",
  expired: "bg-destructive/10 text-destructive",
  unknown: "bg-warning/20 text-warning",
  cancelled: "bg-muted text-muted-foreground",
  verified: "bg-success/15 text-success",
  pass: "bg-success/15 text-success",
  fail: "bg-destructive/15 text-destructive",
  pending: "bg-warning/15 text-warning",
  active: "bg-success/15 text-success",
  revoked: "bg-muted text-muted-foreground",
  sending: "bg-primary/15 text-primary",
  paused: "bg-warning/20 text-warning",
  completed: "bg-success/15 text-success",
  draft: "bg-muted text-foreground",
};

export function StateBadge({ state, label }: { state: string; label?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium",
        tone[state] ?? "bg-muted text-foreground",
      )}
    >
      {label ?? state.replace(/_/g, " ")}
    </span>
  );
}

/** Read and write one URL search parameter; changing it resets the page offset. */
export function useParam(name: string, fallback = "") {
  const [params, setParams] = useSearchParams();
  const value = params.get(name) ?? fallback;
  const set = (v: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (v) next.set(name, v);
        else next.delete(name);
        if (name !== "offset") next.delete("offset");
        return next;
      },
      { replace: false },
    );
  return [value, set] as const;
}

export function Pager({ page }: { page?: Page<unknown> }) {
  const phrase = usePhrase();
  const [offset, setOffset] = useParam("offset", "0");
  const o = Number(offset) || 0;
  if (!page || (o === 0 && !page.has_more)) return null;
  return (
    <nav aria-label="Pagination" className="flex items-center justify-between gap-3 pt-3 text-sm">
      <Button
        variant="outline"
        size="sm"
        disabled={o === 0}
        onClick={() => setOffset(String(Math.max(0, o - page.limit)))}
      >
        {phrase("Previous")}
      </Button>
      <span className="text-muted-foreground">
        {o + 1}–{o + page.items.length}
      </span>
      <Button
        variant="outline"
        size="sm"
        disabled={!page.has_more}
        onClick={() => setOffset(String(o + page.limit))}
      >
        {phrase("Next")}
      </Button>
    </nav>
  );
}

export function useOffset() {
  const [offset] = useParam("offset", "0");
  return Number(offset) || 0;
}

export function Field({
  label,
  error,
  hint,
  children,
  className,
}: {
  label: ReactNode;
  error?: string;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const uid = useId();
  // A single native control gets a real label association, a described-by link to
  // its hint or error, and aria-invalid; anything else keeps the wrapping label.
  if (isValidElement<Record<string, unknown>>(children) && typeof children.type === "string") {
    const inputId = (children.props.id as string | undefined) ?? `${uid}-control`;
    const note = error ? `${uid}-error` : hint ? `${uid}-hint` : undefined;
    return (
      <div className={cn("grid gap-1.5 text-sm", className)}>
        <label htmlFor={inputId} className="font-medium">
          {label}
        </label>
        {cloneElement(children, {
          id: inputId,
          "aria-describedby":
            [children.props["aria-describedby"], note].filter(Boolean).join(" ") || undefined,
          "aria-invalid": error ? true : children.props["aria-invalid"],
        })}
        {hint && !error && (
          <span id={`${uid}-hint`} className="text-xs text-muted-foreground">
            {hint}
          </span>
        )}
        {error && (
          <span id={`${uid}-error`} role="alert" className="text-xs text-destructive">
            {error}
          </span>
        )}
      </div>
    );
  }
  return (
    <label className={cn("grid gap-1.5 text-sm", className)}>
      <span className="font-medium">{label}</span>
      {children}
      {hint && !error && <span className="text-xs text-muted-foreground">{hint}</span>}
      {error && (
        <span role="alert" className="text-xs text-destructive">
          {error}
        </span>
      )}
    </label>
  );
}

export const inputClass =
  "h-9 w-full rounded-md border border-input bg-background px-3 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";
export const selectClass = inputClass + " pr-8";
export const textareaClass =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

export const emailOk = (v: string) => /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(v.trim());

/** A simple responsive table. Rows are clickable when onRow is given. */
export function Table({
  columns,
  rows,
  rowKey,
  onRow,
}: {
  columns: { key: string; label: ReactNode; render?: (r: Row) => ReactNode; className?: string }[];
  rows: Row[];
  rowKey: (r: Row) => string;
  onRow?: (r: Row) => void;
}) {
  return (
    <div className="relative overflow-x-auto rounded-xl border bg-card">
      <table className="w-full text-left text-sm">
        <thead className="border-b bg-muted/40 text-xs uppercase tracking-wide text-muted-foreground">
          <tr>
            {columns.map((c) => (
              <th
                key={c.key}
                scope="col"
                className={cn("whitespace-nowrap px-3 py-2.5 font-medium", c.className)}
              >
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr
              key={rowKey(r)}
              className={cn("border-b last:border-0", onRow && "cursor-pointer hover:bg-muted/50")}
              onClick={
                onRow
                  ? (e) => !(e.target as HTMLElement).closest("a,button,input,select") && onRow(r)
                  : undefined
              }
              onKeyDown={onRow ? (e) => e.key === "Enter" && onRow(r) : undefined}
              tabIndex={onRow ? 0 : undefined}
            >
              {columns.map((c) => (
                <td
                  key={c.key}
                  className={cn("max-w-72 truncate px-3 py-2.5 align-middle", c.className)}
                >
                  {c.render
                    ? c.render(r)
                    : s(r[c.key]) || <span className="text-muted-foreground">—</span>}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** A secret shown exactly once, with a warning. */
export function SecretOnce({
  title,
  value,
  onDone,
}: {
  title: string;
  value: string;
  onDone: () => void;
}) {
  const phrase = usePhrase();
  return (
    <div role="status" className="mb-6 rounded-xl border-2 border-warning bg-warning/10 p-4">
      <p className="flex items-center gap-2 font-semibold">
        <AlertTriangle size={18} aria-hidden /> {title}
      </p>
      <p className="mt-1 text-sm">{phrase("Copy it now. It will not be shown again.")}</p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <code className="break-all rounded bg-background px-2 py-1 text-sm">{value}</code>
        <CopyButton text={value} label="secret" />
      </div>
      <Button className="mt-3" size="sm" variant="outline" onClick={onDone}>
        {phrase("I have stored it")}
      </Button>
    </div>
  );
}
