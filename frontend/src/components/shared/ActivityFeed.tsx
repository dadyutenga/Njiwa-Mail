import * as React from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { formatRelativeTime } from "@/lib/utils";
import type { ActivityItem } from "@/types/dashboard";
import { cn } from "@/lib/utils";

export interface ActivityFeedProps {
  items: ActivityItem[];
  className?: string;
  emptyMessage?: string;
}

export function ActivityFeed({
  items,
  className,
  emptyMessage = "No recent activity",
}: ActivityFeedProps) {
  if (items.length === 0) {
    return (
      <div className="py-6 text-center text-sm text-muted-foreground">
        {emptyMessage}
      </div>
    );
  }

  const getActionBadgeColor = (type: ActivityItem["type"]) => {
    switch (type) {
      case "create":
        return "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
      case "update":
        return "bg-blue-500/10 text-blue-600 dark:text-blue-400";
      case "delete":
        return "bg-rose-500/10 text-rose-600 dark:text-rose-400";
      case "export":
        return "bg-purple-500/10 text-purple-600 dark:text-purple-400";
      default:
        return "bg-muted text-muted-foreground";
    }
  };

  return (
    <div className={cn("flow-root", className)}>
      <ul className="-mb-8">
        {items.map((item, itemIdx) => {
          const initials = item.user.name
            .split(" ")
            .map((n) => n[0])
            .join("")
            .substring(0, 2)
            .toUpperCase();

          return (
            <li key={item.id}>
              <div className="relative pb-8">
                {itemIdx !== items.length - 1 ? (
                  <span
                    className="absolute left-4 top-4 -ml-px h-full w-0.5 bg-border"
                    aria-hidden="true"
                  />
                ) : null}
                <div className="relative flex space-x-3">
                  <div>
                    <Avatar className="h-8 w-8 ring-2 ring-background">
                      {item.user.avatarUrl && (
                        <AvatarImage src={item.user.avatarUrl} alt={item.user.name} />
                      )}
                      <AvatarFallback className="text-[10px] bg-primary/10 text-primary font-semibold">
                        {initials}
                      </AvatarFallback>
                    </Avatar>
                  </div>
                  <div className="flex min-w-0 flex-1 justify-between space-x-4 pt-1.5">
                    <div>
                      <p className="text-xs text-foreground">
                        <span className="font-semibold text-foreground">
                          {item.user.name}
                        </span>{" "}
                        <span
                          className={cn(
                            "rounded px-1.5 py-0.5 font-medium text-[10px]",
                            getActionBadgeColor(item.type),
                          )}
                        >
                          {item.action}
                        </span>{" "}
                        <span className="font-medium text-foreground">{item.target}</span>
                      </p>
                    </div>
                    <div className="whitespace-nowrap text-right text-[11px] text-muted-foreground">
                      <time dateTime={item.timestamp}>
                        {formatRelativeTime(item.timestamp)}
                      </time>
                    </div>
                  </div>
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
