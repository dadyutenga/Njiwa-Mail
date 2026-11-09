import axios, { type AxiosError, type InternalAxiosRequestConfig } from "axios";
import type { ApiError } from "@/types/api";

const BASE_URL = import.meta.env.VITE_API_BASE_URL || "/console";

export const apiClient = axios.create({
  baseURL: BASE_URL,
  timeout: 30000,
  withCredentials: true,
  headers: {
    "Content-Type": "application/json",
    Accept: "application/json",
  },
});

apiClient.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => config,
  (error) => Promise.reject(error),
);

function errorMessage(data: unknown, fallback: string): string {
  if (data && typeof data === "object" && "error" in data) {
    const code = (data as { error?: { code?: string } }).error?.code;
    if (code) {
      return code.split("_").join(" ");
    }
  }
  if (data && typeof data === "object" && "message" in data) {
    const message = (data as { message?: string }).message;
    if (message) return message;
  }
  return fallback;
}

apiClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<{ message?: string; error?: { code?: string } }>) => {
    const url = error.config?.url || "";
    const sessionCall = url.endsWith("/me") || url.endsWith("/login") || url.endsWith("/logout");
    if (error.response?.status === 401 && !sessionCall && !window.location.pathname.startsWith("/login")) {
      window.location.href = `/login?redirect=${encodeURIComponent(window.location.pathname)}`;
    }

    const normalizedError: ApiError = {
      statusCode: error.response?.status,
      message: errorMessage(
        error.response?.data,
        error.message || "An unexpected error occurred. Please try again.",
      ),
    };

    return Promise.reject(normalizedError);
  },
);

export default apiClient;
