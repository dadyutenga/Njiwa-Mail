const base = "/admin/v1";
export type AdminUser = {
  id: string;
  email: string;
  role: "admin";
  csrf_token: string;
  expires_at: string;
};
export type Page<T> = { items: T[]; has_more: boolean; limit: number; offset: number };
let csrf = "";
export function setCsrf(value: string) {
  csrf = value;
}
export class AdminError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
}
export async function adminRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  if (options.method && !["GET", "HEAD"].includes(options.method.toUpperCase()) && csrf)
    headers.set("X-CSRF-Token", csrf);
  let response: Response;
  try {
    response = await fetch(base + path, { ...options, headers, credentials: "same-origin" });
  } catch {
    throw new AdminError(
      "Cannot reach Njiwa-Mail. Check that the backend is running.",
      0,
      "network_error",
    );
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    const error = body?.error;
    if (response.status === 401 && !path.startsWith("/auth/")) {
      csrf = "";
      window.dispatchEvent(new Event("admin-session-expired"));
    }
    throw new AdminError(
      error?.message || error?.code || response.statusText || "Request failed",
      response.status,
      error?.code || "http_error",
    );
  }
  if (response.status === 204) return undefined as T;
  const body = await response.text();
  return (body ? JSON.parse(body) : undefined) as T;
}
export const admin = {
  login: (email: string, password: string) =>
    adminRequest<AdminUser>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  me: () => adminRequest<AdminUser>("/auth/me"),
  logout: () => adminRequest<{ ok: boolean }>("/auth/logout", { method: "POST" }),
  forgot: (email: string) =>
    adminRequest<{ ok: boolean }>("/auth/forgot", {
      method: "POST",
      body: JSON.stringify({ email }),
    }),
  reset: (token: string, password: string) =>
    adminRequest<{ ok: boolean }>("/auth/reset", {
      method: "POST",
      body: JSON.stringify({ token, password }),
    }),
  get: <T>(path: string) => adminRequest<T>(path),
  send: <T>(path: string, method: "POST" | "PATCH" | "PUT" | "DELETE", data?: unknown) =>
    adminRequest<T>(path, { method, body: data === undefined ? undefined : JSON.stringify(data) }),
};
