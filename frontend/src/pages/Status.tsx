import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";

export default function Status({
  code,
  title,
  description,
}: {
  code: string;
  title: string;
  description?: string;
}) {
  const defaultDescriptions: Record<string, string> = {
    "403": "You do not have permission to access this resource or view this page.",
    "404": "The page you are looking for does not exist or may have been moved.",
    "500": "An unexpected server error occurred. Please try again later.",
  };

  return (
    <main className="grid min-h-[70vh] place-items-center p-6 text-center">
      <div className="max-w-md">
        <p className="text-7xl font-extrabold tracking-tight text-primary">{code}</p>
        <h1 className="mt-4 text-2xl font-bold tracking-tight">{title}</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {description ?? defaultDescriptions[code] ?? "An unexpected status occurred."}
        </p>
        <div className="mt-6 flex justify-center gap-3">
          <Button asChild>
            <Link to="/">Return to Dashboard</Link>
          </Button>
        </div>
      </div>
    </main>
  );
}
