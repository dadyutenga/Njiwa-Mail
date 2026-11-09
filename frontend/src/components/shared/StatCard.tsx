import * as React from "react";
import type { LucideIcon } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { KpiTrend } from "./KpiTrend";
import { cn } from "@/lib/utils";

export interface StatCardProps {
  title: string;
  value: string | number;
  change?: number;
  period?: string;
  icon?: LucideIcon;
  description?: string;
  className?: string;
}

export function StatCard({
  title,
  value,
  change,
  period = "from last month",
  icon: Icon,
  description,
  className,
}: StatCardProps) {
  return (
    <Card className={cn("overflow-hidden transition-all hover:shadow-md", className)}>
      <CardContent className="p-5">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium text-muted-foreground">{title}</p>
          {Icon && (
            <div className="rounded-lg bg-primary/10 p-2 text-primary">
              <Icon className="h-5 w-5" aria-hidden="true" />
            </div>
          )}
        </div>
        <div className="mt-2 flex items-baseline gap-2">
          <h3 className="text-2xl font-bold tracking-tight text-foreground">{value}</h3>
          {change !== undefined && <KpiTrend value={change} />}
        </div>
        {(period || description) && (
          <p className="mt-1 text-xs text-muted-foreground">
            {description || period}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
