import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { Spinner } from "@/components/ui/spinner";
import { APP_CONFIG } from "@/lib/constants";

export const NavigationIndicator = () => {
  const location = useLocation();
  const [isNavigating, setIsNavigating] = useState(false);

  useEffect(() => {
    setIsNavigating(true);
    const timer = setTimeout(() => {
      setIsNavigating(false);
    }, 250);

    return () => clearTimeout(timer);
  }, [location.pathname]);

  if (!isNavigating) return null;

  return (
    <div
      role="status"
      aria-label="Loading page navigation"
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-background/60 backdrop-blur-sm transition-opacity"
    >
      <div className="flex flex-col items-center gap-3">
        <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary text-primary-foreground font-bold text-xl shadow-lg animate-pulse">
          T
        </div>
        <Spinner size="md" />
        <span className="text-xs font-medium text-muted-foreground">
          Loading {APP_CONFIG.name}...
        </span>
      </div>
    </div>
  );
};

export default NavigationIndicator;
