import axios, { type AxiosError, type InternalAxiosRequestConfig } from "axios";
import type { ApiError } from "@/types/api";
import { csrfToken } from "@/lib/csrf";

const BASE_URL = import.meta.env.VITE_API_BASE_URL || "/admin/v1";

export const apiClient = axios.create({
  baseURL: BASE_URL,
  timeout: 30000,
  withCredentials: true,
  headers: {
    "Content-Type": "application/json",
    Accept: "application/json",
  },
});

const mutating = new Set(["post", "put", "patch", "delete"]);

apiClient.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const method = (config.method || "get").toLowerCase();
  const url = config.url || "";
  if (mutating.has(method) && !url.endsWith("/auth/login")) {
    config.headers.set("X-CSRF-Token", csrfToken());
  }
  return config;
});

function errorMessage(data: unknown, fallback: string): string {
  if (data && typeof data === "object" && "error" in data) {
    const err = (data as { error?: { code?: string; message?: string } }).error;
    if (err?.message) return err.message;
    if (err?.code) return err.code.split("_").join(" ");
  }
  if (data && typeof data === "object" && "message" in data) {
    const message = (data as { message?: string }).message;
    if (message) return message;
  }
  return fallback;
}

apiClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<{ message?: string; error?: { code?: string; message?: string } }>) => {
    const url = error.config?.url || "";
    const sessionCall =
      url.includes("/auth/me") || url.includes("/auth/login") || url.includes("/auth/logout");
    if (
      error.response?.status === 401 &&
      !sessionCall &&
      !window.location.pathname.startsWith("/login")
    ) {
      window.location.href = `/login?redirect=${encodeURIComponent(window.location.pathname)}`;
    }

    const normalizedError: ApiError = {
      statusCode: error.response?.status,
      message: errorMessage(error.response?.data, error.message || "The request failed."),
    };

    return Promise.reject(normalizedError);
  },
);

export default apiClient;
