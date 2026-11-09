import * as React from "react";
import { Link, useLocation } from "react-router-dom";
import { ChevronRight, Home } from "lucide-react";
import { cn } from "@/lib/utils";

export interface BreadcrumbItem {
  label: string;
  href?: string;
}

export function Breadcrumbs({ className }: { className?: string }) {
  const location = useLocation();

  const pathnames = location.pathname.split("/").filter((x) => x);

  const getLabel = (path: string): string => {
    switch (path) {
      case "dashboard":
        return "Dashboard";
      case "messages":
        return "Messages";
      case "projects":
        return "Projects";
      case "reports":
        return "Reports & Analytics";
      case "settings":
        return "Settings";
      case "profile":
        return "Profile";
      case "example":
        return "Example Page";
      default:
        return path.charAt(0).toUpperCase() + path.slice(1).replace(/-/g, " ");
    }
  };

  return (
    <nav aria-label="Breadcrumb" className={cn("flex items-center text-xs text-muted-foreground", className)}>
      <ol className="flex items-center space-x-1.5">
        <li>
          <Link
            to="/dashboard"
            className="flex items-center text-muted-foreground hover:text-foreground transition-colors"
          >
            <Home className="h-3.5 w-3.5" />
            <span className="sr-only">Home</span>
          </Link>
        </li>

        {pathnames.map((segment, index) => {
          const isLast = index === pathnames.length - 1;
          const href = `/${pathnames.slice(0, index + 1).join("/")}`;
          const label = getLabel(segment);

          return (
            <React.Fragment key={href}>
              <li className="flex items-center">
                <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/60" />
              </li>
              <li>
                {isLast ? (
                  <span className="font-medium text-foreground">{label}</span>
                ) : (
                  <Link
                    to={href}
                    className="hover:text-foreground transition-colors"
                  >
                    {label}
                  </Link>
                )}
              </li>
            </React.Fragment>
          );
        })}
      </ol>
    </nav>
  );
}
