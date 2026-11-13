export type DocsVersion = { id: string; label: string; path: string; status: "latest" | "archived" };

/** Published versions. Add an entry (and a content tree) when a new API version ships; see docs/public-portal/VERSIONING.md. */
export const VERSIONS: DocsVersion[] = [{ id: "1.0", label: "v1 (latest)", path: "/docs", status: "latest" }];

/** The URL of the same page in another version, or that version's root when the page does not exist there. */
export function switchVersion(pathname: string, from: DocsVersion, to: DocsVersion, exists: (path: string) => boolean = () => true): string {
  const rest = pathname.startsWith(from.path) ? pathname.slice(from.path.length) : "";
  const candidate = to.path + rest;
  return exists(candidate) ? candidate : to.path;
}

export function currentVersion(pathname: string, versions: DocsVersion[] = VERSIONS): DocsVersion {
  const sorted = [...versions].sort((a, b) => b.path.length - a.path.length);
  return sorted.find((v) => pathname === v.path || pathname.startsWith(v.path + "/")) ?? versions[0];
}
