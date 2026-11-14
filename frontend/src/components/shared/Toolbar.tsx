import { useEffect, useState } from "react";
import { Bell, Search, SunMoon, UserRound } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { admin } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
import { useAuth } from "@/features/auth/auth";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
type Notice = {
  kind: "announcement" | "daily_report" | "alert";
  id: string;
  title: string;
  body: string;
  link: string;
  created_at: string;
  read: boolean;
  data?: { totals?: Record<string, number>; open_alerts?: number } | null;
};

/** The user's inbox: announcements, daily reports and (operators) open alerts. */
export function NotificationBell() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["notifications"],
    queryFn: () => admin.get<{ items: Notice[]; unread: number }>("/notifications"),
    refetchInterval: 60_000,
  });
  const mark = async (n: Notice, action: "read" | "dismiss") => {
    await admin.send(`/notifications/${n.kind}/${encodeURIComponent(n.id)}/${action}`, "POST");
    client.invalidateQueries({ queryKey: ["notifications"] });
  };
  const unread = q.data?.unread ?? 0;
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="relative"
          aria-label={
            unread
              ? `${phrase("Notifications")} (${unread} ${phrase("unread")})`
              : phrase("Notifications")
          }
        >
          <Bell size={19} />
          {unread > 0 && (
            <span className="absolute right-1 top-1 min-w-4 rounded-full bg-primary px-1 text-center text-[10px] font-semibold leading-4 text-primary-foreground">
              {unread > 9 ? "9+" : unread}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-96 max-w-[calc(100vw-2rem)]">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">{phrase("Notifications")}</h2>
          {unread > 0 && (
            <button
              type="button"
              className="text-xs text-primary"
              onClick={async () => {
                await admin.send("/notifications/read-all", "POST");
                client.invalidateQueries({ queryKey: ["notifications"] });
              }}
            >
              {phrase("Mark all read")}
            </button>
          )}
        </div>
        {q.isPending ? (
          <p className="mt-2 text-sm">{phrase("Loading…")}</p>
        ) : q.error ? (
          <p role="alert" className="mt-2 text-sm">
            {q.error.message}
          </p>
        ) : q.data.items.length === 0 ? (
          <p className="mt-2 text-sm text-muted-foreground">{phrase("Nothing new.")}</p>
        ) : (
          <ul className="mt-2 max-h-96 divide-y overflow-auto">
            {q.data.items.map((n) => (
              <li
                key={`${n.kind}:${n.id}`}
                className={`py-2 text-sm ${n.read ? "opacity-70" : ""}`}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="text-xs uppercase text-muted-foreground">
                      {phrase(
                        n.kind === "daily_report"
                          ? "Daily report"
                          : n.kind === "alert"
                            ? "Alert"
                            : "Announcement",
                      )}
                    </p>
                    <p className={n.read ? "" : "font-semibold"}>
                      {n.link ? (
                        n.link.startsWith("/") ? (
                          <Link
                            to={n.link}
                            onClick={() => mark(n, "read")}
                            className="text-primary"
                          >
                            {n.title}
                          </Link>
                        ) : (
                          <a
                            href={n.link}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-primary"
                            onClick={() => mark(n, "read")}
                          >
                            {n.title}
                          </a>
                        )
                      ) : (
                        n.title
                      )}
                    </p>
                    {n.kind === "daily_report" && n.data?.totals ? (
                      <p className="text-xs text-muted-foreground">
                        {n.data.totals.messages ?? 0} {phrase("messages")} ·{" "}
                        {n.data.totals.submitted ?? 0} {phrase("submitted")} ·{" "}
                        {n.data.totals.failed ?? 0} {phrase("failed")} ·{" "}
                        {n.data.totals.hard_bounces ?? 0} {phrase("hard bounces")} ·{" "}
                        {n.data.open_alerts ?? 0} {phrase("open alerts")}
                      </p>
                    ) : n.kind === "announcement" && n.body ? (
                      <p className="whitespace-pre-wrap text-xs text-muted-foreground">{n.body}</p>
                    ) : null}
                  </div>
                  <span className="flex shrink-0 gap-1">
                    {!n.read && (
                      <button
                        type="button"
                        className="text-xs text-primary"
                        onClick={() => mark(n, "read")}
                      >
                        {phrase("Read")}
                      </button>
                    )}
                    <button
                      type="button"
                      className="text-xs text-muted-foreground"
                      onClick={() => mark(n, "dismiss")}
                    >
                      {phrase("Dismiss")}
                    </button>
                  </span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}
export function ThemeToggle() {
  const [theme, setTheme] = useState(() => localStorage.getItem("theme") ?? "system");
  useEffect(() => {
    const update = () => setTheme(localStorage.getItem("theme") ?? "system");
    window.addEventListener("themechange", update);
    return () => window.removeEventListener("themechange", update);
  }, []);
  const toggle = () => {
    const next = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
    localStorage.setItem("theme", next);
    setTheme(next);
    window.dispatchEvent(new Event("themechange"));
  };
  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggle}
      aria-label={`Theme: ${theme}. Change theme`}
    >
      <SunMoon size={19} />
    </Button>
  );
}
export function UserMenu() {
  const { user, logout } = useAuth();
  const phrase = usePhrase();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" aria-label={phrase("Open user menu")}>
          <UserRound size={19} />
          <span className="hidden sm:inline">{user?.email}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem asChild>
          <Link to="/app?next=profile">{phrase("Profile")}</Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/app?next=settings">{phrase("Settings")}</Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={logout}>{phrase("Sign out")}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
