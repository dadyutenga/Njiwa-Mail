import * as React from "react";
import { APP_CONFIG } from "@/lib/constants";
import { cn } from "@/lib/utils";

export function Footer({ className }: { className?: string }) {
  const year = new Date().getFullYear();

  return (
    <footer
      className={cn(
        "border-t bg-card/50 py-4 px-6 text-xs text-muted-foreground transition-colors",
        className,
      )}
    >
      <div className="flex flex-col sm:flex-row items-center justify-between gap-2">
        <p>
          &copy; {year} {APP_CONFIG.name}. Built with React, Vite & TypeScript.
        </p>
        <div className="flex items-center gap-4">
          <span className="text-[11px] text-muted-foreground/80">
            v{APP_CONFIG.version}
          </span>
          <a
            href="#privacy"
            onClick={(e) => e.preventDefault()}
            className="hover:text-foreground transition-colors"
          >
            Privacy
          </a>
          <a
            href="#terms"
            onClick={(e) => e.preventDefault()}
            className="hover:text-foreground transition-colors"
          >
            Terms
          </a>
          <a
            href="#support"
            onClick={(e) => e.preventDefault()}
            className="hover:text-foreground transition-colors"
          >
            Support
          </a>
        </div>
      </div>
    </footer>
  );
}
