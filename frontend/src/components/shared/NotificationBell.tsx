import * as React from "react";
import { Bell, Check, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { formatRelativeTime } from "@/lib/utils";

interface NotificationItem {
  id: string;
  title: string;
  description: string;
  time: string;
  read: boolean;
}

const INITIAL_NOTIFICATIONS: NotificationItem[] = [
  {
    id: "1",
    title: "System Update Complete",
    description: "Template initialized to React 18, Vite, and strict TypeScript.",
    time: new Date(Date.now() - 1000 * 60 * 15).toISOString(),
    read: false,
  },
  {
    id: "2",
    title: "New Record Assigned",
    description: "Item #REC-1049 was assigned to your review queue.",
    time: new Date(Date.now() - 1000 * 60 * 90).toISOString(),
    read: false,
  },
  {
    id: "3",
    title: "Weekly Analytics Ready",
    description: "Your periodic summary report has been compiled and is ready for export.",
    time: new Date(Date.now() - 1000 * 60 * 60 * 6).toISOString(),
    read: true,
  },
];

export function NotificationBell() {
  const [notifications, setNotifications] = React.useState<NotificationItem[]>(
    INITIAL_NOTIFICATIONS,
  );

  const unreadCount = notifications.filter((n) => !n.read).length;

  const markAllRead = () => {
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
  };

  const clearAll = () => {
    setNotifications([]);
  };

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="relative h-9 w-9 text-muted-foreground hover:text-foreground"
          aria-label="View notifications"
        >
          <Bell className="h-4 w-4" />
          {unreadCount > 0 && (
            <span className="absolute right-1.5 top-1.5 flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
            </span>
          )}
          <span className="sr-only">Notifications</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-80 p-0" align="end">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="font-semibold text-sm">Notifications</span>
            {unreadCount > 0 && (
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-bold text-primary">
                {unreadCount} new
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            {unreadCount > 0 && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-xs text-muted-foreground hover:text-foreground"
                onClick={markAllRead}
                title="Mark all as read"
              >
                <Check className="h-3.5 w-3.5" />
              </Button>
            )}
            {notifications.length > 0 && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-xs text-muted-foreground hover:text-destructive"
                onClick={clearAll}
                title="Clear all notifications"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        </div>

        <div className="max-h-72 overflow-y-auto divide-y">
          {notifications.length === 0 ? (
            <div className="py-8 text-center text-xs text-muted-foreground">
              No notifications yet
            </div>
          ) : (
            notifications.map((item) => (
              <div
                key={item.id}
                className={`p-3 text-xs transition-colors hover:bg-muted/50 ${
                  !item.read ? "bg-primary/5" : ""
                }`}
              >
                <div className="flex items-start justify-between gap-1">
                  <span className="font-semibold text-foreground">{item.title}</span>
                  <span className="text-[10px] text-muted-foreground shrink-0">
                    {formatRelativeTime(item.time)}
                  </span>
                </div>
                <p className="mt-1 text-muted-foreground leading-relaxed">
                  {item.description}
                </p>
              </div>
            ))
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
