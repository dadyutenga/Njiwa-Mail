import * as React from "react";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

export interface PageLoaderProps {
  message?: string;
  className?: string;
}

export function PageLoader({ message = "Loading...", className }: PageLoaderProps) {
  return (
    <div
      className={cn(
        "flex min-h-[50vh] flex-col items-center justify-center p-8 text-center",
        className,
      )}
    >
      <Spinner size="lg" />
      {message && (
        <p className="mt-4 text-sm font-medium text-muted-foreground animate-pulse">
          {message}
        </p>
      )}
    </div>
  );
}
