import React from "react";
import { createBrowserRouter, Navigate } from "react-router-dom";
import { routesConfig } from "./routes.config";
import { DashboardLayout } from "@/components/layout/DashboardLayout";
import { AuthLayout } from "@/components/layout/AuthLayout";
import { ProtectedRoute } from "@/components/ProtectedRoute";
import { ErrorBoundary } from "@/components/shared/ErrorBoundary";
import { PageLoader } from "@/components/shared/PageLoader";

/**
 * Builds the modern React Router data router directly from `routesConfig`.
 * Supports DashboardLayout, AuthLayout, and blank layouts with code-splitting
 * and error boundaries.
 */
export const router = createBrowserRouter([
  // 1. Dashboard Layout Routes (Protected by default)
  {
    element: (
      <ProtectedRoute>
        <DashboardLayout />
      </ProtectedRoute>
    ),
    errorElement: (
      <DashboardLayout>
        <ErrorBoundary />
      </DashboardLayout>
    ),
    children: routesConfig
      .filter((r) => r.layout === "dashboard")
      .map((route) => ({
        path: route.path.replace(/^\//, ""),
        element: (
          <React.Suspense fallback={<PageLoader />}>
            {route.roles && route.roles.length > 0 ? (
              <ProtectedRoute allowedRoles={route.roles}>
                {route.element}
              </ProtectedRoute>
            ) : (
              route.element
            )}
          </React.Suspense>
        ),
      })),
  },

  // 2. Auth Layout Routes (Public authentication)
  {
    element: <AuthLayout />,
    errorElement: (
      <AuthLayout>
        <ErrorBoundary />
      </AuthLayout>
    ),
    children: routesConfig
      .filter((r) => r.layout === "auth")
      .map((route) => ({
        path: route.path.replace(/^\//, ""),
        element: (
          <React.Suspense fallback={<PageLoader />}>
            {route.element}
          </React.Suspense>
        ),
      })),
  },

  // 3. Blank / Standalone Routes (Landing, Error pages)
  ...routesConfig
    .filter((r) => r.layout === "blank")
    .map((route) => ({
      path: route.path === "/" ? "" : route.path.replace(/^\//, ""),
      element: (
        <React.Suspense fallback={<PageLoader />}>
          {route.element}
        </React.Suspense>
      ),
      errorElement: <ErrorBoundary />,
    })),

  // Fallback catch-all
  {
    path: "*",
    element: <Navigate to="/404" replace />,
  },
]);
