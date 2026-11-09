import * as React from "react";
import { TrendingUp, TrendingDown, Minus } from "lucide-react";
import { cn } from "@/lib/utils";

export interface KpiTrendProps {
  value: number;
  suffix?: string;
  className?: string;
  showIcon?: boolean;
}

export function KpiTrend({
  value,
  suffix = "%",
  className,
  showIcon = true,
}: KpiTrendProps) {
  const isPositive = value > 0;
  const isNegative = value < 0;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium",
        isPositive && "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
        isNegative && "bg-rose-500/10 text-rose-600 dark:text-rose-400",
        !isPositive && !isNegative && "bg-muted text-muted-foreground",
        className,
      )}
    >
      {showIcon && (
        <>
          {isPositive && <TrendingUp className="h-3 w-3 shrink-0" />}
          {isNegative && <TrendingDown className="h-3 w-3 shrink-0" />}
          {!isPositive && !isNegative && <Minus className="h-3 w-3 shrink-0" />}
        </>
      )}
      <span>
        {isPositive && "+"}
        {value}
        {suffix}
      </span>
    </span>
  );
}
