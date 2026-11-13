import Link from "next/link";

export default function NotFound() {
  return (
    <main id="main" className="mx-auto flex max-w-xl flex-1 flex-col items-center justify-center gap-4 px-4 py-24 text-center">
      <p className="text-sm font-semibold uppercase tracking-wide text-fd-muted-foreground">404</p>
      <h1 className="text-3xl font-bold">This page does not exist</h1>
      <p className="text-fd-muted-foreground">It may have moved, or the link may be wrong. Try the quickstart or search from the docs.</p>
      <div className="flex gap-3">
        <Link className="rounded-md bg-fd-primary px-3 py-2 text-sm font-medium text-fd-primary-foreground" href="/docs">
          Documentation home
        </Link>
        <Link className="rounded-md border px-3 py-2 text-sm font-medium" href="/docs/quickstart">
          Quickstart
        </Link>
      </div>
    </main>
  );
}
