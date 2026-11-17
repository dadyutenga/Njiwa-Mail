import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { BrandIcon } from "@/components/shared/BrandIcon";
import { usePhrase } from "@/features/i18n/i18n";

export function AuthLayout({ children }: { children: ReactNode }) {
  const phrase = usePhrase();
  return (
    <main className="grid min-h-screen place-items-center p-4">
      <div className="w-full max-w-md">
        <Link
          to="/"
          className="mb-3 inline-flex items-center gap-1.5 rounded-md px-1 py-1 text-sm font-medium text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
        >
          <ArrowLeft size={16} aria-hidden="true" />
          {phrase("Back to home")}
        </Link>
        <section className="w-full rounded-2xl border bg-card p-8 shadow-xl">
          <Link to="/" className="mb-7 inline-flex items-center gap-2.5 font-bold text-primary">
            <BrandIcon size={40} />
            <span>Njiwa-Mail</span>
          </Link>
          {children}
        </section>
      </div>
    </main>
  );
}
