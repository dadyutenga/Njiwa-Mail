"use client";
import { usePathname, useRouter } from "next/navigation";
import { VERSIONS, currentVersion, switchVersion } from "@/lib/versions";

export function VersionSelect() {
  const pathname = usePathname();
  const router = useRouter();
  const cur = currentVersion(pathname);
  return (
    <label className="inline-flex items-center gap-1 text-xs text-fd-muted-foreground">
      <span className="sr-only">Documentation version</span>
      <select
        data-testid="version-select"
        className="rounded-md border bg-fd-background px-1.5 py-1 text-sm text-fd-foreground"
        value={cur.id}
        onChange={(e) => {
          const to = VERSIONS.find((v) => v.id === e.target.value);
          if (to) router.push(switchVersion(pathname, cur, to));
        }}
      >
        {VERSIONS.map((v) => (
          <option key={v.id} value={v.id}>
            {v.label}
          </option>
        ))}
      </select>
    </label>
  );
}
