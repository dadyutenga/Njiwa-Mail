// Public, non-secret links. Empty values hide their associated links.
// VITE_DOCS_URL is the origin of the public documentation site (apps/docs/, see docs/public-portal/DEPLOY.md);
// there is no central hosted docs site, so it is empty unless the operator publishes one.
export const welcomeLinks = {
  github: import.meta.env.VITE_GITHUB_URL ?? "https://github.com/dadyutenga/Njiwa-Mail",
  docs: import.meta.env.VITE_DOCS_URL ?? "",
};

/**
 * VITE_DOCS_URL is the docs site's origin; its pages live under /docs. Link
 * there, not to the bare origin (which, on a shared domain, is this page).
 */
export function docsHome(origin: string = welcomeLinks.docs): string {
  return origin ? origin.replace(/\/+$/, "") + "/docs" : "";
}

/** Same rule as apps/docs/scripts/generate-reference.mjs, so links match its pages. */
const kebab = (s: string) =>
  s
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/[^A-Za-z0-9]+/g, "-")
    .toLowerCase()
    .replace(/^-|-$/g, "");

type OpenAPI = {
  paths?: Record<string, Record<string, { tags?: string[]; operationId?: string }>>;
};

/** "POST /v1/emails" -> its API reference page, from the published openapi.json. */
export function referencePages(spec: OpenAPI, home: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const [path, item] of Object.entries(spec.paths ?? {}))
    for (const [method, op] of Object.entries(item ?? {})) {
      if (!op?.operationId) continue;
      const tag = op.tags?.[0] ?? "Other";
      out.set(
        `${method.toUpperCase()} ${path}`,
        `${home}/api-reference/${kebab(tag)}/${kebab(op.operationId)}`,
      );
    }
  return out;
}
