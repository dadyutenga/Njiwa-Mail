import { useEffect, useState } from "react";
import { Bell, Search, SunMoon, UserRound } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { admin, type Page } from "@/lib/admin-api";
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
export function NotificationBell() {
  const phrase = usePhrase();
  const q = useQuery({
    queryKey: ["alerts", "open"],
    queryFn: () => admin.get<Page<Record<string, unknown>>>("/alerts?open=true&limit=5"),
  });
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={phrase("Notifications")}>
          <Bell size={19} />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <h2 className="font-semibold">{phrase("Notifications")}</h2>
        {q.isPending ? (
          <p className="mt-2 text-sm">{phrase("Loading…")}</p>
        ) : q.error ? (
          <p role="alert" className="mt-2 text-sm">
            {q.error.message}
          </p>
        ) : (
          <p className="mt-2 text-sm text-muted-foreground">
            {q.data.items.length} {phrase("open alerts")}
          </p>
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
