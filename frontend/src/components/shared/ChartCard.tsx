import * as React from "react";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export interface ChartCardProps {
  title: string;
  description?: string;
  action?: React.ReactNode;
  isLoading?: boolean;
  className?: string;
  children: React.ReactNode;
}

export function ChartCard({
  title,
  description,
  action,
  isLoading,
  className,
  children,
}: ChartCardProps) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
        <div>
          <CardTitle className="text-base font-semibold">{title}</CardTitle>
          {description && (
            <CardDescription className="text-xs">{description}</CardDescription>
          )}
        </div>
        {action && <div>{action}</div>}
      </CardHeader>
      <CardContent className="pt-0">
        {isLoading ? (
          <Skeleton className="h-[280px] w-full rounded-md" />
        ) : (
          children
        )}
      </CardContent>
    </Card>
  );
}
