import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { BrandIcon } from "@/components/shared/BrandIcon";
import { usePhrase } from "@/features/i18n/i18n";

export default function Status({
  code,
  title,
  description,
}: {
  code: string;
  title: string;
  description?: string;
}) {
  const phrase = usePhrase();
  const defaultDescriptions: Record<string, string> = {
    "403": "You do not have permission to access this resource or view this page.",
    "404": "The page you are looking for does not exist or may have been moved.",
    "500": "An unexpected server error occurred. Please try again later.",
  };

  return (
    <main className="grid min-h-[70vh] place-items-center p-6 text-center">
      <div className="max-w-md">
        <Link
          to="/"
          className="mb-6 inline-flex items-center gap-2 font-bold"
          aria-label={phrase("Njiwa-Mail home")}
        >
          <BrandIcon size={40} />
          <span>Njiwa-Mail</span>
        </Link>
        <p className="text-7xl font-extrabold tracking-tight text-primary">{code}</p>
        <h1 className="mt-4 text-2xl font-bold tracking-tight">{phrase(title)}</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {phrase(description ?? defaultDescriptions[code] ?? "An unexpected status occurred.")}
        </p>
        <div className="mt-6 flex justify-center gap-3">
          <Button asChild>
            <Link to="/dashboard">{phrase("Return to Dashboard")}</Link>
          </Button>
        </div>
      </div>
    </main>
  );
}
