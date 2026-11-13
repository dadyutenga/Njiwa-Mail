"use client";
import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Copy, ExternalLink } from "lucide-react";
import { AI_SERVICES, buildPrompt, isAllowedExternal, isPublicOrigin } from "@/lib/ai-services";
import { copyText } from "@/lib/clipboard";
import { useToast } from "./toast";

/** Copy Markdown and Open ▾, under the page title. Every action is an explicit user click. */
export function PageActions({ markdownPath }: { markdownPath: string }) {
  const toast = useToast();
  const [copied, setCopied] = useState(false);
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        root.current?.querySelector<HTMLElement>("[data-open-trigger]")?.focus();
      }
    };
    const onClick = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  const copyMarkdown = async () => {
    try {
      const res = await fetch(markdownPath, { headers: { Accept: "text/markdown" } });
      if (!res.ok) throw new Error(String(res.status));
      const ok = await copyText(await res.text());
      if (!ok) throw new Error("clipboard");
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
      toast("Markdown copied");
    } catch {
      toast("Could not copy the Markdown. Open the .md link instead.", "error");
    }
  };

  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const markdownUrl = origin + markdownPath;
  const prompt = buildPrompt(markdownUrl, origin + "/openapi.yaml");
  const linkable = isPublicOrigin(origin);

  const choose = async (id: string) => {
    const svc = AI_SERVICES.find((s) => s.id === id);
    if (!svc) return;
    setOpen(false);
    const target = linkable && svc.mode === "prefill" && svc.prefillUrl ? svc.prefillUrl(prompt) : svc.homepage;
    if (!isAllowedExternal(target)) {
      toast("That link is not allowed.", "error");
      return;
    }
    if (target !== svc.homepage) {
      window.open(target, "_blank", "noopener,noreferrer");
      return;
    }
    // Start the copy and open the window in the same click: a window opened
    // after an await is no longer a user action and may be blocked as a popup.
    const copying = copyText(prompt);
    window.open(target, "_blank", "noopener,noreferrer");
    const ok = await copying;
    toast(ok ? `Prompt copied. Paste it into ${svc.name}.` : "Could not copy the prompt.", ok ? "ok" : "error");
  };

  const btn =
    "inline-flex items-center gap-1.5 rounded-md border bg-fd-secondary px-2.5 py-1.5 text-sm font-medium text-fd-secondary-foreground hover:bg-fd-accent focus-visible:outline-2 focus-visible:outline-fd-ring";

  return (
    <div ref={root} className="not-prose relative mb-6 flex flex-wrap items-center gap-2" data-testid="page-actions">
      <button type="button" className={btn} onClick={copyMarkdown} data-testid="copy-markdown">
        {copied ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
        Copy Markdown
      </button>
      <div>
        <button
          type="button"
          className={btn}
          aria-haspopup="menu"
          aria-expanded={open}
          data-open-trigger
          data-testid="open-menu"
          onClick={() => setOpen((v) => !v)}
        >
          Open
          <ChevronDown size={14} aria-hidden="true" />
        </button>
        {open && (
          <div role="menu" aria-label="Open this page in an AI service" className="absolute left-0 top-full z-50 mt-1 w-72 max-w-full rounded-lg border bg-fd-popover p-1 text-sm shadow-lg">
            {AI_SERVICES.map((s) => (
              <button
                key={s.id}
                type="button"
                role="menuitem"
                data-testid={`open-${s.id}`}
                className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left hover:bg-fd-accent"
                onClick={() => void choose(s.id)}
              >
                <ExternalLink size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
                <span>
                  Open in {s.name}
                  <span className="block text-xs text-fd-muted-foreground">
                    {linkable && s.mode === "prefill" ? "Opens with a short prompt that links to this page" : "Copies a prompt, then opens the site"}
                  </span>
                </span>
              </button>
            ))}
            <a role="menuitem" className="block rounded-md px-2 py-1.5 hover:bg-fd-accent" href={markdownPath} data-testid="view-markdown">
              View as Markdown
            </a>
            <a role="menuitem" className="block rounded-md px-2 py-1.5 hover:bg-fd-accent" href="/openapi.yaml">
              View OpenAPI
            </a>
          </div>
        )}
      </div>
    </div>
  );
}
