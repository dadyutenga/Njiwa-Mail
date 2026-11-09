import * as React from "react";
import { Outlet, Link } from "react-router-dom";
import { ThemeToggle } from "@/components/shared/ThemeToggle";
import { Button } from "@/components/ui/button";
import { useLanguage } from "@/contexts/LanguageContext";
import { APP_CONFIG } from "@/lib/constants";

export function AuthLayout() {
  const { language, toggleLanguage } = useLanguage();

  return (
    <div className="min-h-screen bg-muted/20 flex flex-col justify-between">
      {/* Top Header */}
      <header className="flex items-center justify-between px-6 py-4">
        <Link to="/" className="flex items-center gap-2">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-primary-foreground font-bold text-base shadow-sm">
            T
          </div>
          <span className="font-bold text-lg tracking-tight text-foreground">
            {APP_CONFIG.name}
          </span>
        </Link>

        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={toggleLanguage}
            className="text-xs h-8 px-2 font-semibold"
          >
            {language === "en" ? "SW" : "EN"}
          </Button>
          <ThemeToggle />
        </div>
      </header>

      {/* Centered Auth Card Container */}
      <main className="flex-1 flex items-center justify-center p-4 sm:p-6">
        <div className="w-full max-w-md rounded-2xl border bg-card p-6 sm:p-8 shadow-sm text-card-foreground animate-fade-in">
          <Outlet />
        </div>
      </main>

      {/* Bottom Footer */}
      <footer className="py-4 text-center text-xs text-muted-foreground">
        <p>&copy; {new Date().getFullYear()} {APP_CONFIG.name}. All rights reserved.</p>
      </footer>
    </div>
  );
}

export default AuthLayout;
