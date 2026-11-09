import * as React from "react";
import { Link } from "react-router-dom";
import { ShieldAlert, Home } from "lucide-react";
import { Button } from "@/components/ui/button";

export function ForbiddenPage() {
  return (
    <div className="flex min-h-[70vh] flex-col items-center justify-center p-6 text-center animate-fade-in">
      <div className="rounded-full bg-rose-500/10 p-5 text-rose-500 mb-4">
        <ShieldAlert className="h-10 w-10" />
      </div>
      <span className="font-mono text-5xl font-extrabold text-foreground sm:text-6xl">
        403
      </span>
      <h1 className="mt-3 text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
        Access Forbidden
      </h1>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">
        You don&apos;t have the required administrative role or permissions to access this protected area.
      </p>
      <div className="mt-6 flex gap-3">
        <Button size="sm" asChild className="gap-2">
          <Link to="/dashboard">
            <Home className="h-4 w-4" />
            Return to Dashboard
          </Link>
        </Button>
      </div>
    </div>
  );
}

export default ForbiddenPage;
