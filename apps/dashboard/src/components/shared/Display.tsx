import type { ReactNode } from "react";
import { LoaderCircle } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
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
export function StatCard({
  label,
  value,
  trend,
}: {
  label: string;
  value: ReactNode;
  trend?: string;
}) {
  return (
    <section className="rounded-2xl border bg-card p-5">
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className="mt-4 text-3xl font-semibold">{value}</p>
      {trend && <TrendIndicator value={trend} />}
    </section>
  );
}
export function TrendIndicator({ value }: { value: string }) {
  return <p className="mt-2 text-xs text-primary">{value}</p>;
}
export function ChartCard({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-2xl border bg-card p-6">
      <h2 className="font-semibold">{title}</h2>
      {description && <p className="text-sm text-muted-foreground">{description}</p>}
      <div className="mt-6">{children}</div>
    </section>
  );
}
export type Activity = { id: string; label: string; time: string };
export function ActivityFeed({ items }: { items: Activity[] }) {
  return (
    <ol className="divide-y">
      {items.map((item) => (
        <li key={item.id} className="flex justify-between gap-4 py-4 text-sm">
          <span>{item.label}</span>
          <time className="text-muted-foreground">{item.time}</time>
        </li>
      ))}
    </ol>
  );
}
export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <span role="status" aria-label={label}>
      <LoaderCircle className="animate-spin" aria-hidden="true" />
    </span>
  );
}
export function PageLoader() {
  return (
    <div className="grid min-h-48 place-items-center">
      <Spinner />
    </div>
  );
}
export function EmptyState({ title, description }: { title: string; description?: string }) {
  return (
    <div className="p-8 text-center">
      <p className="font-semibold">{title}</p>
      {description && <p className="mt-2 text-sm text-muted-foreground">{description}</p>}
    </div>
  );
}
export function ConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  title,
  description,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  title: string;
  description: string;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm}>Confirm</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
export function CopyButton({ text }: { text: string }) {
  return (
    <Button
      variant="outline"
      onClick={() => {
        void navigator.clipboard.writeText(text).then(
          () => toast.success("Copied"),
          () => toast.error("Could not copy"),
        );
      }}
    >
      Copy
    </Button>
  );
}
export function Stepper({ steps, current }: { steps: string[]; current: number }) {
  return (
    <ol className="flex flex-wrap gap-3" aria-label="Progress">
      {steps.map((step, index) => (
        <li
          key={step}
          aria-current={index === current ? "step" : undefined}
          className={index === current ? "font-semibold text-primary" : "text-muted-foreground"}
        >
          {index + 1}. {step}
        </li>
      ))}
    </ol>
  );
}
export function PrintableDocument({ title, children }: { title: string; children: ReactNode }) {
  return (
    <article className="print-document rounded-xl border bg-card p-8">
      <div className="flex justify-between">
        <h1 className="text-2xl font-bold">{title}</h1>
        <Button className="print:hidden" variant="outline" onClick={() => window.print()}>
          Print
        </Button>
      </div>
      <div className="mt-6">{children}</div>
    </article>
  );
}
