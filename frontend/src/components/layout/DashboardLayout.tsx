import * as React from "react";
import { Outlet, NavLink, useLocation } from "react-router-dom";
import {
  Menu,
  ChevronLeft,
  ChevronRight,
  Search,
  Globe,
  Sparkles,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTrigger } from "@/components/ui/sheet";
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";
import { Breadcrumbs } from "./Breadcrumbs";
import { Footer } from "./Footer";
import { ThemeToggle } from "@/components/shared/ThemeToggle";
import { UserMenu } from "@/components/shared/UserMenu";
import { NotificationBell } from "@/components/shared/NotificationBell";
import { CommandPalette } from "@/components/shared/CommandPalette";
import { PageLoader } from "@/components/shared/PageLoader";
import { useLanguage } from "@/contexts/LanguageContext";
import { useAuth } from "@/contexts/AuthContext";
import { getNavSections } from "@/app/routes.config";
import { APP_CONFIG, STORAGE_KEYS } from "@/lib/constants";
import { cn } from "@/lib/utils";

export function DashboardLayout() {
  const location = useLocation();
  const { user } = useAuth();
  const { language, toggleLanguage } = useLanguage();
  const navSections = getNavSections(user?.role);

  const [isCollapsed, setIsCollapsed] = React.useState<boolean>(() => {
    try {
      return localStorage.getItem(STORAGE_KEYS.sidebar_collapsed) === "true";
    } catch {
      return false;
    }
  });

  const [mobileOpen, setMobileOpen] = React.useState(false);
  const [commandPaletteOpen, setCommandPaletteOpen] = React.useState(false);

  const toggleSidebar = () => {
    setIsCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(STORAGE_KEYS.sidebar_collapsed, String(next));
      } catch (e) {
        console.warn("Failed to persist sidebar state", e);
      }
      return next;
    });
  };

  return (
    <TooltipProvider delayDuration={150}>
      <div className="min-h-screen bg-background text-foreground flex">
        {/* Desktop Collapsible Sidebar */}
        <aside
          className={cn(
            "hidden md:flex flex-col border-r bg-card transition-all duration-300 z-30",
            isCollapsed ? "w-18" : "w-64",
          )}
        >
          {/* Brand Header */}
          <div className="h-16 flex items-center justify-between px-4 border-b">
            <NavLink to="/dashboard" className="flex items-center gap-2.5 overflow-hidden">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground font-bold text-base shadow-sm">
                M
              </div>
              {!isCollapsed && (
                <div className="flex flex-col">
                  <span className="font-bold text-sm tracking-tight text-foreground truncate">
                    {APP_CONFIG.name}
                  </span>
                  <span className="text-[10px] text-muted-foreground uppercase font-semibold tracking-wider">
                    Operator
                  </span>
                </div>
              )}
            </NavLink>

            <Button
              variant="ghost"
              size="icon"
              onClick={toggleSidebar}
              className="h-7 w-7 text-muted-foreground hover:text-foreground shrink-0"
              aria-label={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
            >
              {isCollapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
            </Button>
          </div>

          {/* Navigation Links */}
          <div className="flex-1 overflow-y-auto py-4 px-3 space-y-6">
            {navSections.map((section) => (
              <div key={section.id} className="space-y-1">
                {!isCollapsed && (
                  <p className="px-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {section.title}
                  </p>
                )}
                <nav className="space-y-1">
                  {section.items.map((item) => {
                    const Icon = item.icon;
                    const isActive = item.path === "/dashboard"
                      ? location.pathname === item.path
                      : location.pathname === item.path || location.pathname.startsWith(`${item.path}/`);

                    const linkContent = (
                      <NavLink
                        to={item.path}
                        className={cn(
                          "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                          isActive
                            ? "bg-primary text-primary-foreground font-semibold shadow-sm"
                            : "text-muted-foreground hover:bg-muted/80 hover:text-foreground",
                          isCollapsed && "justify-center px-2",
                        )}
                      >
                        {Icon && <Icon className="h-4 w-4 shrink-0" />}
                        {!isCollapsed && (
                          <span className="flex-1 truncate">{item.title}</span>
                        )}
                        {!isCollapsed && item.badge && (
                          <Badge
                            variant={isActive ? "secondary" : "outline"}
                            className="ml-auto text-[10px] h-5 px-1.5 font-normal"
                          >
                            {item.badge}
                          </Badge>
                        )}
                      </NavLink>
                    );

                    if (isCollapsed) {
                      return (
                        <Tooltip key={item.path}>
                          <TooltipTrigger asChild>{linkContent}</TooltipTrigger>
                          <TooltipContent side="right">
                            <p className="font-medium text-xs">{item.title}</p>
                          </TooltipContent>
                        </Tooltip>
                      );
                    }

                    return <div key={item.path}>{linkContent}</div>;
                  })}
                </nav>
              </div>
            ))}
          </div>

          {/* Quick Version Banner */}
          {!isCollapsed && (
            <div className="p-3 m-3 rounded-xl border bg-muted/40 text-xs">
              <div className="flex items-center gap-1.5 font-semibold text-foreground">
                <Sparkles className="h-3.5 w-3.5 text-primary" />
                <span>Read-only console</span>
              </div>
              <p className="text-[11px] text-muted-foreground mt-1">
                Project keys stay on the server.
              </p>
            </div>
          )}
        </aside>

        {/* Main Application Area */}
        <div className="flex-1 flex flex-col min-w-0">
          {/* Topbar */}
          <header className="h-16 border-b bg-card/60 backdrop-blur sticky top-0 z-20 flex items-center justify-between px-4 sm:px-6">
            <div className="flex items-center gap-3">
              {/* Mobile Drawer Trigger */}
              <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
                <SheetTrigger asChild>
                  <Button variant="ghost" size="icon" className="md:hidden h-9 w-9">
                    <Menu className="h-5 w-5" />
                    <span className="sr-only">Toggle navigation menu</span>
                  </Button>
                </SheetTrigger>
                <SheetContent side="left" className="w-72 p-0 flex flex-col">
                  <div className="h-16 flex items-center justify-between px-6 border-b">
                    <div className="flex items-center gap-2.5">
                      <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-primary-foreground font-bold text-base">
                        T
                      </div>
                      <span className="font-bold text-base">{APP_CONFIG.name}</span>
                    </div>
                  </div>
                  <div className="flex-1 overflow-y-auto p-4 space-y-6">
                    {navSections.map((section) => (
                      <div key={section.id} className="space-y-1">
                        <p className="px-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                          {section.title}
                        </p>
                        <nav className="space-y-1">
                          {section.items.map((item) => {
                            const Icon = item.icon;
                            const isActive = item.path === "/dashboard"
                      ? location.pathname === item.path
                      : location.pathname === item.path || location.pathname.startsWith(`${item.path}/`);

                            return (
                              <NavLink
                                key={item.path}
                                to={item.path}
                                onClick={() => setMobileOpen(false)}
                                className={cn(
                                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                                  isActive
                                    ? "bg-primary text-primary-foreground font-semibold"
                                    : "text-muted-foreground hover:bg-muted/80 hover:text-foreground",
                                )}
                              >
                                {Icon && <Icon className="h-4 w-4" />}
                                <span>{item.title}</span>
                                {item.badge && (
                                  <Badge
                                    variant={isActive ? "secondary" : "outline"}
                                    className="ml-auto text-[10px]"
                                  >
                                    {item.badge}
                                  </Badge>
                                )}
                              </NavLink>
                            );
                          })}
                        </nav>
                      </div>
                    ))}
                  </div>
                </SheetContent>
              </Sheet>

              {/* Breadcrumbs */}
              <div className="hidden sm:block">
                <Breadcrumbs />
              </div>
            </div>

            {/* Right Actions Toolbar */}
            <div className="flex items-center gap-2">
              {/* Command Palette Trigger */}
              <button
                type="button"
                onClick={() => setCommandPaletteOpen(true)}
                className="hidden md:flex items-center gap-2 h-9 w-56 rounded-lg border bg-muted/40 px-3 text-xs text-muted-foreground hover:bg-muted/70 transition-colors"
              >
                <Search className="h-3.5 w-3.5" />
                <span className="flex-1 text-left">Search or type cmd...</span>
                <kbd className="pointer-events-none rounded border bg-background px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
                  ⌘K
                </kbd>
              </button>

              <Button
                variant="ghost"
                size="icon"
                onClick={() => setCommandPaletteOpen(true)}
                className="md:hidden h-9 w-9 text-muted-foreground"
                aria-label="Search"
              >
                <Search className="h-4 w-4" />
              </Button>

              {/* Language Switcher Button */}
              <Button
                variant="ghost"
                size="sm"
                onClick={toggleLanguage}
                className="text-xs h-9 px-2.5 font-semibold text-muted-foreground hover:text-foreground"
                title={`Switch language (current: ${language.toUpperCase()})`}
              >
                <Globe className="h-3.5 w-3.5 mr-1" />
                <span>{language === "en" ? "SW" : "EN"}</span>
              </Button>

              {/* Notification Bell */}
              <NotificationBell />

              {/* Theme Toggle (Light/Dark/System) */}
              <ThemeToggle />

              {/* User Menu Dropdown */}
              <UserMenu />
            </div>
          </header>

          {/* Main Content Area */}
          <main className="flex-1 p-4 sm:p-6 lg:p-8 max-w-7xl w-full mx-auto">
            <React.Suspense fallback={<PageLoader message="Loading content..." />}>
              <Outlet />
            </React.Suspense>
          </main>

          {/* Footer */}
          <Footer />
        </div>

        {/* Global Command Palette Dialog */}
        <CommandPalette open={commandPaletteOpen} onOpenChange={setCommandPaletteOpen} />
      </div>
    </TooltipProvider>
  );
}

export default DashboardLayout;
