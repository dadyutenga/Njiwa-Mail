import { Suspense } from "react";
import { createBrowserRouter } from "react-router-dom";
import DashboardLayout from "@/components/layout/DashboardLayout";
import { ProtectedRoute } from "@/features/auth/auth";
import { routes } from "./routes";
import AuthPage from "@/pages/Auth";
import Status from "@/pages/Status";
import { PageLoader } from "@/components/shared/Display";
import { ErrorBoundary, NotFound } from "@/components/shared/RouteErrors";

export const router = createBrowserRouter([
  {
    path: "/",
    element: (
      <ProtectedRoute>
        <DashboardLayout />
      </ProtectedRoute>
    ),
    errorElement: <ErrorBoundary />,
    children: [
      ...routes.map(({ path, component: Component }) => ({
        index: path === "/" ? true : undefined,
        path: path === "/" ? undefined : path.slice(1),
        element: (
          <Suspense fallback={<PageLoader />}>
            <Component />
          </Suspense>
        ),
      })),
    ],
  },
  ...["login", "register", "forgot-password", "reset-password"].map((path) => ({
    path,
    element: <AuthPage mode={path} />,
  })),
  { path: "403", element: <Status code="403" title="Access Denied" /> },
  { path: "404", element: <Status code="404" title="Page Not Found" /> },
  { path: "500", element: <Status code="500" title="Server Error" /> },
  { path: "*", element: <NotFound /> },
]);
