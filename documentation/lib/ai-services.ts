import { DOCS_VERSION } from "./site";

/**
 * "Open in …" targets. This file is the only place URL formats live.
 *
 * `prefill`: the service opens with the prompt filled in (the user still presses send).
 * `copy`: no reliable prefill format is known, so the prompt is copied and the provider's
 * homepage opens after the user's explicit click.
 *
 * Evidence is community or reference-implementation documentation, NOT provider-official:
 * re-check each format in a browser before a release (docs/public-portal/RELEASE_CHECKLIST.md).
 */
export type AiService = {
  id: string;
  name: string;
  mode: "prefill" | "copy";
  homepage: string;
  prefillUrl?: (prompt: string) => string;
  evidence: string;
  checkedOn: string;
};

export const AI_SERVICES: AiService[] = [
  {
    id: "chatgpt",
    name: "ChatGPT",
    mode: "prefill",
    homepage: "https://chatgpt.com/",
    prefillUrl: (p) => `https://chatgpt.com/?${new URLSearchParams({ q: p })}`,
    evidence: "chatgpt.com/?q= is the widely used prefill pattern (community documentation); not an OpenAI-published contract.",
    checkedOn: "2026-11-13",
  },
  {
    id: "claude",
    name: "Claude",
    mode: "copy",
    homepage: "https://claude.ai/new",
    evidence: "Reports say claude.ai ?q= prefill stopped working in 2025, so the prompt is copied instead.",
    checkedOn: "2026-11-13",
  },
  {
    id: "scira",
    name: "Scira AI",
    mode: "prefill",
    homepage: "https://scira.ai/",
    prefillUrl: (p) => `https://scira.ai/?${new URLSearchParams({ q: p })}`,
    evidence: "Listed with a q parameter by the AI SDK 'Open In Chat' reference component; not Scira-published.",
    checkedOn: "2026-11-13",
  },
  {
    id: "t3chat",
    name: "T3 Chat",
    mode: "prefill",
    homepage: "https://t3.chat/",
    prefillUrl: (p) => `https://t3.chat/new?${new URLSearchParams({ q: p })}`,
    evidence: "t3.chat/new?q= appears in user reports and the AI SDK reference component; not T3-published.",
    checkedOn: "2026-11-13",
  },
];

/** Hosts a menu action may ever open. Anything else is refused. */
export const ALLOWED_HOSTS = ["chatgpt.com", "claude.ai", "scira.ai", "t3.chat"];

export function isAllowedExternal(url: string): boolean {
  try {
    const u = new URL(url);
    return u.protocol === "https:" && !u.username && !u.password && ALLOWED_HOSTS.includes(u.hostname);
  } catch {
    return false;
  }
}

/** A public, secret-free prompt that points at the page, never one that embeds the documentation. */
export function buildPrompt(markdownUrl: string, openapiUrl: string): string {
  return [
    `Read ${markdownUrl} (Njiwa-Mail documentation, version ${DOCS_VERSION}) and answer my questions from it.`,
    `The machine-readable API contract is ${openapiUrl}.`,
    "Use only the endpoints, fields and states they document. If something is not documented, say so instead of guessing.",
    "Never put API keys, passwords or webhook secrets in code that runs in a browser.",
  ].join(" ");
}

/** Prompts whose page cannot be fetched by an outside service (local or private hosts) are copied, not linked. */
export function isPublicOrigin(origin: string): boolean {
  try {
    const h = new URL(origin).hostname;
    if (h === "localhost" || h.endsWith(".local") || h.endsWith(".internal")) return false;
    if (/^(127\.|10\.|192\.168\.|169\.254\.)/.test(h) || /^172\.(1[6-9]|2\d|3[01])\./.test(h) || h === "[::1]") return false;
    return true;
  } catch {
    return false;
  }
}
