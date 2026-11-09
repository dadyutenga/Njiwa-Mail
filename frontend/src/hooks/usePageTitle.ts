import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { routesConfig } from "@/app/routes.config";
import { APP_CONFIG } from "@/lib/constants";

export const usePageTitle = () => {
  const location = useLocation();

  useEffect(() => {
    const matchingRoute = routesConfig.find(
      (r) => r.path === location.pathname || (r.path.includes(":") && location.pathname.startsWith(r.path.split(":")[0])),
    );

    const title = matchingRoute?.title
      ? `${matchingRoute.title} | ${APP_CONFIG.name}`
      : APP_CONFIG.name;

    document.title = title;
  }, [location.pathname]);
};

export default usePageTitle;
