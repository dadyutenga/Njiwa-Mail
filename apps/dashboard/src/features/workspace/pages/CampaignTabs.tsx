import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { usePhrase } from "@/features/i18n/i18n";
import { cn } from "@/lib/utils";
import { useWorkspace } from "../context";

const tabs: [string, string][] = [
  ["contacts", "Audience"],
  ["bulk", "Campaigns"],
  ["scheduled", "Scheduled"],
  ["newsletters", "Newsletters"],
];

/** The four parts of Campaigns & Audience, linked to each other. */
export function Tabbed({ children }: { children: ReactNode }) {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  return (
    <>
      <nav
        aria-label={phrase("Campaigns & Audience")}
        className="mb-4 flex gap-1 overflow-x-auto border-b"
      >
        {tabs.map(([path, label]) => (
          <NavLink
            key={path}
            to={to(path)}
            className={({ isActive }) =>
              cn(
                "-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm",
                isActive
                  ? "border-primary font-semibold"
                  : "border-transparent text-muted-foreground",
              )
            }
          >
            {phrase(label)}
          </NavLink>
        ))}
      </nav>
      {children}
    </>
  );
}
