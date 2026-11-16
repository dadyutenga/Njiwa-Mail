import "./global.css";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import { RootProvider } from "fumadocs-ui/provider/next";
import { ToastProvider } from "@/components/toast";
import { DOCS_BASE_URL } from "@/lib/site";

export const metadata: Metadata = {
  metadataBase: new URL(DOCS_BASE_URL),
  title: { default: "Njiwa-Mail documentation", template: "%s | Njiwa-Mail" },
  description: "Send transactional email from your own server. Developer and AI-agent documentation for the Njiwa-Mail API.",
  alternates: { types: { "text/markdown": "/llms.txt" } },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <RootProvider search={{ options: { type: "static" } }}>
          <ToastProvider>{children}</ToastProvider>
        </RootProvider>
      </body>
    </html>
  );
}
