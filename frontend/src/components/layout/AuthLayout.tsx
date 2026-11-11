import type { ReactNode } from "react";
import { Link } from "react-router-dom";
export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <main className="grid min-h-screen place-items-center p-4">
      <section className="w-full max-w-md rounded-2xl border bg-card p-8 shadow-xl">
        <Link to="/" className="font-bold text-primary">
          Your App
        </Link>
        {children}
      </section>
    </main>
  );
}
