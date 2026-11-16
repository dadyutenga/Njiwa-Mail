import { describe, expect, it } from "vitest";
import { mdxToMarkdown } from "../scripts/lib/md.mjs";
import { lintPage } from "../scripts/lib/lint.mjs";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { AI_SERVICES, ALLOWED_HOSTS, buildPrompt, isAllowedExternal, isPublicOrigin } from "../lib/ai-services";
import { VERSIONS, currentVersion, switchVersion, type DocsVersion } from "../lib/versions";

describe("mdxToMarkdown", () => {
  it("turns tabs, steps and callouts into plain Markdown and makes links absolute", () => {
    const md = mdxToMarkdown(
      ['<Callout type="warn" title="Careful">', "Do not do it. See [x](/docs/quickstart#go).", "</Callout>", '<Tabs items={["a"]}>', '<Tab value="curl">', "", "```bash", "curl x", "```", "", "</Tab>", "</Tabs>", "[spec](/openapi.yaml) and [home](/docs)"].join("\n"),
      "https://docs.example.com",
    );
    expect(md).not.toMatch(/<\/?(Callout|Tabs?|Steps?)/);
    expect(md).toContain("> **Careful**");
    expect(md).toContain("> Do not do it.");
    expect(md).toContain("**curl**");
    expect(md).toContain("[x](https://docs.example.com/docs/quickstart.md#go)");
    expect(md).toContain("[spec](https://docs.example.com/openapi.yaml)");
    expect(md).toContain("[home](https://docs.example.com/docs/index.md)");
    expect(md).toContain("```bash\ncurl x\n```");
  });
});

function lintText(text: string): string[] {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "lint-"));
  const file = path.join(dir, "page.mdx");
  fs.writeFileSync(file, text);
  return lintPage(file, new Map([["/docs/quickstart", new Set(["go"])]]));
}
const page = (body: string, fm = 'title: "A page"\ndescription: "A long enough description."') => `---\n${fm}\n---\n\n${body}\n`;

describe("content lint (MDX is code)", () => {
  it("accepts ordinary content", () => expect(lintText(page("Hello [q](/docs/quickstart#go) and `{x}`."))).toEqual([]));
  const cases: [string, string][] = [
    ["import x from 'y'", "import/export"],
    ["export const a = 1", "import/export"],
    ["Value {process.env.SECRET}", "curly braces"],
    ["<script>alert(1)</script>", "not allowed"],
    ["<iframe src='https://x'></iframe>", "not allowed"],
    ['<Callout onClick="x">hi</Callout>', "event-handler"],
    ["[bad](javascript:alert(1))", "unsafe link"],
    ["[bad](data:text/html,x)", "unsafe link"],
    ["[bad](http://example.com)", "must be https"],
    ["[bad](//evil.example)", "protocol-relative"],
    ["[bad](/docs/nope)", "broken internal link"],
    ["[bad](/docs/quickstart#missing)", "no heading"],
    ["[bad](relative.md)", "absolute"],
    ["Your email was delivered to the inbox.", "claim delivery"],
    ["Njiwa-Mail requires Mailcow to work.", "Mailcow is optional"],
    ["From Ditronics Ltd", "Ditronics"],
    ["Use https://mail.njiwa.co.tz here", "example.com"],
    ["curl http://localhost:8080/v1/emails", "placeholder"],
    ["key dmk_liv_abcdef123456_0123456789abcdef0123", "API key"],
    ["secret whsec_0123456789abcdef0123456789", "webhook secret"],
    ["-----BEGIN RSA PRIVATE KEY-----", "private key"],
    ["```\nno language\n```", "no language"],
    ["```cobol\nx\n```", "unknown language"],
  ];
  for (const [body, expected] of cases) it(`rejects: ${body.split("\n")[0].slice(0, 40)}`, () => expect(lintText(page(body)).join("\n")).toContain(expected));
  it("allows negated delivery statements", () => expect(lintText(page("It does not mean delivered, and there is no delivered state."))).toEqual([]));
  it("requires a title and description", () => expect(lintText(page("x", "title: A"))).not.toEqual([]));
});

describe("AI services", () => {
  it("only ever opens allowlisted https hosts", () => {
    expect(isAllowedExternal("https://chatgpt.com/?q=x")).toBe(true);
    for (const bad of ["http://chatgpt.com/", "https://evil.example/?q=chatgpt.com", "https://chatgpt.com.evil.example/", "javascript:alert(1)", "https://user:pw@chatgpt.com/", "https://chatgpt.com@evil.example/"]) expect(isAllowedExternal(bad)).toBe(false);
  });
  it("every service URL stays on its own allowlisted host and carries only the short public prompt", () => {
    const prompt = buildPrompt("https://docs.example.com/docs/quickstart.md", "https://docs.example.com/openapi.yaml");
    expect(prompt.length).toBeLessThan(800);
    for (const s of AI_SERVICES) {
      expect(isAllowedExternal(s.homepage)).toBe(true);
      expect(s.evidence.length).toBeGreaterThan(10);
      if (s.mode === "prefill") {
        const url = s.prefillUrl!(prompt);
        expect(isAllowedExternal(url)).toBe(true);
        expect(ALLOWED_HOSTS).toContain(new URL(url).hostname);
        expect(url.length).toBeLessThan(2000);
        expect(decodeURIComponent(url.split("q=")[1].replace(/\+/g, " "))).toBe(prompt);
      } else expect(s.prefillUrl).toBeUndefined();
    }
  });
  it("the prompt links to the docs instead of embedding them, and holds no secret", () => {
    const prompt = buildPrompt("https://docs.example.com/docs/x.md", "https://docs.example.com/openapi.yaml");
    expect(prompt).toContain("https://docs.example.com/docs/x.md");
    expect(prompt).not.toMatch(/dmk_|whsec_|BEGIN/);
  });
  it("does not offer outside services a local or private page", () => {
    for (const o of ["http://localhost:3000", "http://127.0.0.1:3000", "http://192.168.1.4", "http://10.0.0.2", "http://172.20.1.1", "https://docs.internal", "https://docs.local"]) expect(isPublicOrigin(o)).toBe(false);
    expect(isPublicOrigin("https://docs.example.com")).toBe(true);
  });
});

describe("versions", () => {
  const v1: DocsVersion = { id: "1.0", label: "v1", path: "/docs", status: "latest" };
  const v0: DocsVersion = { id: "0.9", label: "v0.9", path: "/docs/0.9", status: "archived" };
  it("lists the published versions and finds the current one", () => {
    expect(VERSIONS.length).toBeGreaterThan(0);
    expect(currentVersion("/docs/api/send-email", [v1, v0]).id).toBe("1.0");
    expect(currentVersion("/docs/0.9/api/send-email", [v1, v0]).id).toBe("0.9");
  });
  it("switches to the same page, or the version root when it does not exist there", () => {
    expect(switchVersion("/docs/api/send-email", v1, v0)).toBe("/docs/0.9/api/send-email");
    expect(switchVersion("/docs/0.9/api/batch", v0, v1)).toBe("/docs/api/batch");
    expect(switchVersion("/docs/api/new-page", v1, v0, () => false)).toBe("/docs/0.9");
  });
});
