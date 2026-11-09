import * as React from "react";
import { Link } from "react-router-dom";
import { Home, ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";

export function NotFoundPage() {
  return (
    <div className="flex min-h-[70vh] flex-col items-center justify-center p-6 text-center animate-fade-in">
      <span className="font-mono text-7xl font-extrabold text-primary/30 sm:text-9xl">
        404
      </span>
      <h1 className="mt-4 text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
        Page Not Found
      </h1>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">
        Sorry, the page you are looking for doesn&apos;t exist or has been moved to another URL.
      </p>
      <div className="mt-6 flex flex-wrap gap-3 justify-center">
        <Button variant="outline" size="sm" onClick={() => window.history.back()} className="gap-2">
          <ArrowLeft className="h-4 w-4" />
          Go Back
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

export default NotFoundPage;
