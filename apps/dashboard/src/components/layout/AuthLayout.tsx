import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { BrandIcon } from "@/components/shared/BrandIcon";
export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <main className="grid min-h-screen place-items-center p-4">
      <section className="w-full max-w-md rounded-2xl border bg-card p-8 shadow-xl">
        <Link to="/" className="mb-7 inline-flex items-center gap-2.5 font-bold text-primary">
          <BrandIcon size={40} />
          <span>Njiwa-Mail</span>
        </Link>
        {children}
      </section>
    </main>
  );
}
