import * as React from "react";
import { Link } from "react-router-dom";
import {
  Sparkles,
  ArrowRight,
  ShieldCheck,
  Zap,
  BarChart2,
  Table,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { ThemeToggle } from "@/components/shared/ThemeToggle";
import { APP_CONFIG } from "@/lib/constants";

export function LandingPage() {
  return (
    <div className="min-h-screen bg-background text-foreground flex flex-col">
      {/* Top Navbar */}
      <header className="border-b bg-card/50 backdrop-blur sticky top-0 z-40">
        <div className="container mx-auto flex h-16 items-center justify-between px-4 sm:px-6">
          <div className="flex items-center gap-2.5">
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-primary-foreground font-bold text-base shadow-sm">
              T
            </div>
            <span className="font-bold text-lg tracking-tight">{APP_CONFIG.name}</span>
          </div>

          <div className="flex items-center gap-3">
            <ThemeToggle />
            <Button variant="ghost" size="sm" asChild>
              <Link to="/login">Sign In</Link>
            </Button>
            <Button size="sm" asChild>
              <Link to="/dashboard">Dashboard</Link>
            </Button>
          </div>
        </div>
      </header>

      {/* Hero Section */}
      <main className="flex-1">
        <section className="container mx-auto px-4 py-20 sm:py-28 text-center max-w-4xl">
          <div className="inline-flex items-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-3.5 py-1 text-xs font-semibold text-primary mb-6">
            <Sparkles className="h-3.5 w-3.5" />
            <span>React + Vite + TypeScript Template</span>
          </div>

          <h1 className="text-4xl sm:text-6xl font-extrabold tracking-tight text-foreground leading-[1.15]">
            The Modern Foundation for Your Next Frontend Project
          </h1>

          <p className="mt-5 text-base sm:text-xl text-muted-foreground max-w-2xl mx-auto leading-relaxed">
            Enterprise-grade dashboard layouts, full CRUD tables, Recharts visualizations, accessible UI primitives, and clean role-based auth.
          </p>

          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <Button size="lg" asChild className="gap-2 h-11 px-6">
              <Link to="/dashboard">
                <span>Explore Live Dashboard</span>
                <ArrowRight className="h-4 w-4" />
              </Link>
            </Button>
            <Button variant="outline" size="lg" asChild className="h-11 px-6">
              <Link to="/messages">View messages</Link>
            </Button>
          </div>

          {/* Quick Highlights */}
          <div className="mt-14 grid grid-cols-2 sm:grid-cols-4 gap-4 text-left">
            <div className="rounded-xl border bg-card p-4">
              <Zap className="h-5 w-5 text-primary mb-2" />
              <h3 className="font-semibold text-sm">Vite & Strict TS</h3>
              <p className="text-xs text-muted-foreground mt-1">Instant HMR, strict type safety</p>
            </div>
            <div className="rounded-xl border bg-card p-4">
              <Table className="h-5 w-5 text-primary mb-2" />
              <h3 className="font-semibold text-sm">Rich DataTable</h3>
              <p className="text-xs text-muted-foreground mt-1">Sort, filter, search, export CSV</p>
            </div>
            <div className="rounded-xl border bg-card p-4">
              <BarChart2 className="h-5 w-5 text-primary mb-2" />
              <h3 className="font-semibold text-sm">Recharts Ready</h3>
              <p className="text-xs text-muted-foreground mt-1">Area, bar, line & pie charts</p>
            </div>
            <div className="rounded-xl border bg-card p-4">
              <ShieldCheck className="h-5 w-5 text-primary mb-2" />
              <h3 className="font-semibold text-sm">Auth & Roles</h3>
              <p className="text-xs text-muted-foreground mt-1">Scaffolded mock auth & guards</p>
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t py-6 text-center text-xs text-muted-foreground">
        <p>&copy; {new Date().getFullYear()} {APP_CONFIG.name}. Ready for production.</p>
      </footer>
    </div>
  );
}

export default LandingPage;
