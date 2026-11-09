import * as React from "react";
import { Link } from "react-router-dom";
import { ServerCrash, RefreshCw, Home } from "lucide-react";
import { Button } from "@/components/ui/button";

export function ServerErrorPage() {
  return (
    <div className="flex min-h-[70vh] flex-col items-center justify-center p-6 text-center animate-fade-in">
      <div className="rounded-full bg-amber-500/10 p-5 text-amber-500 mb-4">
        <ServerCrash className="h-10 w-10" />
      </div>
      <span className="font-mono text-5xl font-extrabold text-foreground sm:text-6xl">
        500
      </span>
      <h1 className="mt-3 text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
        Internal Server Error
      </h1>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">
        Our backend encountered an unexpected condition. Please refresh or check back in a moment.
      </p>
      <div className="mt-6 flex flex-wrap gap-3 justify-center">
        <Button variant="outline" size="sm" onClick={() => window.location.reload()} className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Reload Page
        </Button>
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

export default ServerErrorPage;
