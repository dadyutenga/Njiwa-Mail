import { useEffect, useState } from "react";
import { Bell, Search, SunMoon, UserRound } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { admin, type Page } from "@/lib/admin-api";
import { routes } from "@/app/routes";
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
export function SearchPalette() {
  const { user } = useAuth();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const navigate = useNavigate();
  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen(true);
      }
    };
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, []);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="icon" variant="ghost" aria-label="Search pages">
          <Search size={19} />
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>Search pages</DialogTitle>
        <DialogDescription>
          Find a page by name or press Control K to open search.
        </DialogDescription>
        <Input
          autoFocus
          aria-label="Search pages"
          placeholder="Search pages…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <ul className="max-h-64 overflow-auto">
          {routes
            .filter((route) => route.navigation && (!route.role || user?.role === route.role))
            .filter((route) => route.label.toLowerCase().includes(query.toLowerCase()))
            .map((route) => (
              <li key={route.path}>
                <button
                  className="w-full rounded p-3 text-left hover:bg-muted focus-visible:bg-muted"
                  onClick={() => {
                    setOpen(false);
                    setQuery("");
                    navigate(route.path);
                  }}
                >
                  {route.label}
                </button>
              </li>
            ))}
        </ul>
      </DialogContent>
    </Dialog>
  );
}
export function NotificationBell() {
  const q = useQuery({
    queryKey: ["alerts", "open"],
    queryFn: () => admin.get<Page<Record<string, unknown>>>("/alerts?open=true&limit=5"),
  });
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Notifications">
          <Bell size={19} />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <h2 className="font-semibold">Notifications</h2>
        {q.isPending ? (
          <p className="mt-2 text-sm">Loading…</p>
        ) : q.error ? (
          <p role="alert" className="mt-2 text-sm">
            {q.error.message}
          </p>
        ) : (
          <p className="mt-2 text-sm text-muted-foreground">{q.data.items.length} open alerts</p>
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
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" aria-label="Open user menu">
          <UserRound size={19} />
          <span className="hidden sm:inline">{user?.email}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem asChild>
          <Link to="/profile">Profile</Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/settings">Settings</Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={logout}>Sign out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
