import { useState } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import { Menu, PanelLeftClose } from "lucide-react";
import { routes } from "@/app/routes";
import {
  NotificationBell,
  SearchPalette,
  ThemeToggle,
  UserMenu,
} from "@/components/shared/Toolbar";
import { LanguageSwitcher } from "@/features/i18n/LanguageSwitcher";
import { useI18n } from "@/features/i18n/i18n";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

function Navigation({
  compact = false,
  onNavigate,
}: {
  compact?: boolean;
  onNavigate?: () => void;
}) {
  return (
    <nav aria-label="Main navigation" className="space-y-1">
      {routes
        .filter((route) => route.navigation)
        .map(({ path, label, icon: Icon }) => (
          <NavLink
            end={path === "/"}
            onClick={onNavigate}
            key={path}
            to={path}
            title={compact ? label : undefined}
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition ${
                isActive
                  ? "bg-primary/15 text-primary"
                  : "text-muted-foreground hover:bg-muted hover:text-foreground"
              }`
            }
          >
            <Icon size={19} />
            {!compact && <span>{label}</span>}
          </NavLink>
        ))}
    </nav>
  );
}

export default function DashboardLayout() {
  const [compact, setCompact] = useState(false);
  const [open, setOpen] = useState(false);
  const location = useLocation();
  const { t } = useI18n();

  const currentRoute = routes.find((r) => r.path === location.pathname);
  const title = currentRoute?.label ?? "Page";

  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* Desktop Collapsible Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-20 hidden border-r bg-card p-4 transition-all md:block ${
          compact ? "w-20" : "w-64"
        }`}
      >
        <Link to="/dashboard" className="mb-8 flex h-10 items-center gap-3 px-2 font-bold">
          <span className="grid h-9 w-9 place-items-center rounded-xl bg-primary text-sm font-black text-primary-foreground">
            A
          </span>
          {!compact && <span className="text-base tracking-tight">Your App</span>}
        </Link>
        <Navigation compact={compact} />
      </aside>

      {/* Main Content Area */}
      <div className={compact ? "md:pl-20" : "md:pl-64"}>
        <header className="sticky top-0 z-10 flex h-16 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur sm:px-8">
          {/* Mobile Sheet Drawer */}
          <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="md:hidden"
                aria-label="Open navigation"
              >
                <Menu />
              </Button>
            </SheetTrigger>
            <SheetContent side="left">
              <SheetTitle className="mb-6">Your App</SheetTitle>
              <SheetDescription className="sr-only">Choose a page to navigate to.</SheetDescription>
              <Navigation onNavigate={() => setOpen(false)} />
            </SheetContent>
          </Sheet>

          {/* Sidebar Collapse Toggle */}
          <Button
            variant="ghost"
            size="icon"
            className="hidden md:inline-flex"
            aria-label="Toggle sidebar"
            aria-expanded={!compact}
            onClick={() => setCompact(!compact)}
          >
            <PanelLeftClose />
          </Button>

          {/* Breadcrumbs */}
          <div className="hidden sm:block">
            <Breadcrumb>
              <BreadcrumbList>
                <BreadcrumbItem>
                  <BreadcrumbLink asChild>
                    <Link to="/dashboard">{t.nav.home}</Link>
                  </BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbPage>{title}</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
          </div>

          {/* Topbar Actions */}
          <div className="ml-auto flex items-center gap-1.5 sm:gap-2">
            <SearchPalette />
            <LanguageSwitcher />
            <ThemeToggle />
            <NotificationBell />
            <UserMenu />
          </div>
        </header>

        <main className="mx-auto max-w-7xl p-4 sm:p-8">
          <Outlet />
        </main>

        <footer className="border-t px-8 py-6 text-xs text-muted-foreground">
          Your App · Reusable Frontend Template
        </footer>
      </div>
    </div>
  );
}
