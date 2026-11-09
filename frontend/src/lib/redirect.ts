/** Same-origin path only. Rejects protocol-relative and absolute URLs. */
export function safeRedirect(value: string | null | undefined, fallback = "/dashboard"): string {
  if (!value || !value.startsWith("/") || value.startsWith("//") || value.includes("\\") || value.includes("://")) {
    return fallback;
  }
  return value;
}
