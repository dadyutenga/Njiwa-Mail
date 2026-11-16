// Generates everything agents fetch, from the one content source and the canonical OpenAPI file:
//   public/docs/**.md   public/llms.txt   public/llms-full.txt
//   public/openapi.yaml public/openapi.json   public/skills/**   public/versions.json
// Everything carries the same documentation version and source commit.
import fs from "node:fs";
import path from "node:path";
import YAML from "yaml";
import { REPO, ROOT, mdPathOf, orderedFiles, parsePage, urlOf } from "./lib/content.mjs";
import { mdxToMarkdown } from "./lib/md.mjs";

export const VERSION = "1.0";
const BASE = (process.env.NEXT_PUBLIC_DOCS_BASE_URL ?? "https://docs.example.com").replace(/\/$/, "");
const COMMIT = process.env.NEXT_PUBLIC_DOCS_COMMIT ?? "dev";
const PUB = path.join(ROOT, "public");

function reset() {
  for (const p of ["docs", "skills"]) fs.rmSync(path.join(PUB, p), { recursive: true, force: true });
  for (const f of ["llms.txt", "llms-full.txt", "openapi.yaml", "openapi.json", "versions.json"]) fs.rmSync(path.join(PUB, f), { force: true });
  fs.mkdirSync(PUB, { recursive: true });
}

function write(rel, text) {
  const f = path.join(PUB, rel);
  fs.mkdirSync(path.dirname(f), { recursive: true });
  fs.writeFileSync(f, text);
}

export function pageMarkdown(page, file) {
  const planned = page.fm.status === "planned" ? "> **PLANNED.** This page describes something that is not implemented yet. Do not build against it.\n\n" : "";
  return (
    `# ${page.fm.title}\n\n> ${page.fm.description}\n\n` +
    `Source: ${BASE}${urlOf(file)} (Njiwa-Mail documentation ${VERSION}, commit ${COMMIT})\n\n` +
    planned +
    mdxToMarkdown(page.body, BASE)
  );
}

function main() {
  reset();
  const files = orderedFiles();
  const pages = files.map((f) => ({ file: f, page: parsePage(f) }));
  const full = [];
  const index = [];
  for (const { file, page } of pages) {
    if (!page.fm) throw new Error(`${file}: no frontmatter`);
    const md = pageMarkdown(page, file);
    write(mdPathOf(file).slice(1), md);
    full.push(md);
    index.push(`- [${page.fm.title}](${BASE}${mdPathOf(file)}): ${page.fm.description}${page.fm.status === "planned" ? " (PLANNED, not implemented)" : ""}`);
  }
  const openapiSrc = fs.readFileSync(path.join(REPO, "docs", "openapi.yaml"), "utf8");
  write("openapi.yaml", openapiSrc);
  write("openapi.json", JSON.stringify(YAML.parse(openapiSrc), null, 2) + "\n");
  const skillDir = path.join(REPO, "skills", "njiwa-mail-integration");
  const skillFiles = [];
  if (fs.existsSync(skillDir)) {
    const walk = (d) => {
      for (const n of fs.readdirSync(d)) {
        const f = path.join(d, n);
        if (fs.statSync(f).isDirectory()) walk(f);
        else if (n.endsWith(".md")) {
          const rel = "skills/njiwa-mail-integration/" + path.relative(skillDir, f).replace(/\\/g, "/");
          write(rel, fs.readFileSync(f, "utf8"));
          skillFiles.push(rel);
        }
      }
    };
    walk(skillDir);
  }
  const header =
    `# Njiwa-Mail\n\n> Njiwa-Mail is a self-hosted service for transactional email (OTP, password reset, notices). One HTTP API; mail leaves through managed Postfix on your VPS or through an existing SMTP server. \`submitted\` means your mail server accepted the message, never that it was delivered.\n\n` +
    `Documentation version ${VERSION}, source commit ${COMMIT}. Examples use https://mail-api.example.com: replace it with your own instance URL (there is no central hosted endpoint). Use only the endpoints, fields and states documented here; if something is not documented, say so.\n\n`;
  write(
    "llms.txt",
    header +
      `## Docs\n\n${index.join("\n")}\n\n## Machine-readable\n\n- [OpenAPI (YAML)](${BASE}/openapi.yaml): the public API contract\n- [OpenAPI (JSON)](${BASE}/openapi.json)\n- [All docs in one file](${BASE}/llms-full.txt)\n` +
      (skillFiles.length ? `\n## Agent skill\n\n${skillFiles.map((s) => `- [${s.split("/").pop()}](${BASE}/${s})`).join("\n")}\n` : ""),
  );
  write("llms-full.txt", header + full.join("\n\n---\n\n"));
  write("versions.json", JSON.stringify({ current: VERSION, commit: COMMIT, versions: [{ id: VERSION, path: "/docs", status: "latest" }] }, null, 2) + "\n");
  console.log(`generated ${pages.length} pages, llms.txt, llms-full.txt, openapi.{yaml,json}, ${skillFiles.length} skill file(s)`);
}

if (import.meta.url === `file://${process.argv[1]}`) main();
