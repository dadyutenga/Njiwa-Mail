import type { ReactNode } from "react";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { VersionSelect } from "@/components/version-select";
import { source } from "@/lib/source";

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsLayout tree={source.getPageTree()} nav={{ title: "Njiwa-Mail", url: "/", children: <VersionSelect /> }}>
      {children}
    </DocsLayout>
  );
}
