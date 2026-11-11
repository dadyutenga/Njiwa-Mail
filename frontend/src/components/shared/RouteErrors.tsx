import { Link } from "react-router-dom";
import { BrandIcon } from "@/components/shared/BrandIcon";

function HomeBrand() {
  return (
    <Link
      to="/"
      className="mb-6 inline-flex items-center gap-2 font-bold"
      aria-label="Njiwa-Mail home"
    >
      <BrandIcon size={40} />
      <span>Njiwa-Mail</span>
    </Link>
  );
}
export function NotFound() {
  return (
    <main className="grid min-h-screen place-items-center p-6">
      <div className="text-center">
        <HomeBrand />
        <p className="text-6xl font-bold text-primary">404</p>
        <h1 className="mt-4 text-2xl font-semibold">Page not found</h1>
        <Link className="mt-6 inline-block text-primary underline" to="/">
          Return home
        </Link>
      </div>
    </main>
  );
}
/** React Router renders this when a route loader or render throws. */
export function ErrorBoundary() {
  return (
    <main className="grid min-h-screen place-items-center p-6">
      <div className="text-center">
        <HomeBrand />
        <p className="text-6xl font-bold text-primary">500</p>
        <h1 className="mt-4 text-2xl font-semibold">Something went wrong</h1>
        <Link className="mt-6 inline-block text-primary underline" to="/">
          Return home
        </Link>
      </div>
    </main>
  );
}
