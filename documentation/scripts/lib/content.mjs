// Shared helpers: read the content tree in navigation order, parse pages, slugs and headings.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import YAML from "yaml";

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
export const CONTENT = path.join(ROOT, "content", "docs");
export const REPO = path.resolve(ROOT, "..");

export function readMeta(dir) {
  const f = path.join(dir, "meta.json");
  return fs.existsSync(f) ? JSON.parse(fs.readFileSync(f, "utf8")) : { pages: [] };
}

/** Pages in sidebar order. Separators ("---Title---") are skipped; "...dir" expands a folder. */
export function orderedFiles(dir = CONTENT) {
  const meta = readMeta(dir);
  const out = [];
  const seen = new Set();
  const take = (rel) => {
    const file = path.join(dir, rel + ".mdx");
    if (fs.existsSync(file) && !seen.has(file)) {
      seen.add(file);
      out.push(file);
    }
  };
  for (const entry of meta.pages ?? []) {
    if (/^---.*---$/.test(entry)) continue;
    if (entry.startsWith("...")) {
      for (const f of orderedFiles(path.join(dir, entry.slice(3)))) if (!seen.has(f)) (seen.add(f), out.push(f));
    } else take(entry);
  }
  // Anything not in a meta file is appended alphabetically so it is never silently dropped.
  for (const name of fs.readdirSync(dir).sort()) {
    const full = path.join(dir, name);
    if (fs.statSync(full).isFile() && name.endsWith(".mdx") && !seen.has(full)) (seen.add(full), out.push(full));
    if (fs.statSync(full).isDirectory() && !(meta.pages ?? []).some((p) => p === "..." + name)) {
      for (const f of orderedFiles(full)) if (!seen.has(f)) (seen.add(f), out.push(f));
    }
  }
  return out;
}

export function slugOf(file) {
  const rel = path.relative(CONTENT, file).replace(/\\/g, "/").replace(/\.mdx$/, "");
  return rel === "index" ? "" : rel;
}
export const urlOf = (file) => "/docs" + (slugOf(file) ? "/" + slugOf(file) : "");
export const mdPathOf = (file) => "/docs/" + (slugOf(file) || "index") + ".md";

export function parsePage(file) {
  const raw = fs.readFileSync(file, "utf8");
  const m = raw.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!m) return { file, raw, fm: null, body: raw };
  let fm = null;
  try {
    fm = YAML.parse(m[1]);
  } catch {
    fm = null;
  }
  return { file, raw, fm, body: m[2] };
}

export function githubSlug(text) {
  return text
    .toLowerCase()
    .replace(/`/g, "")
    .replace(/<[^>]+>/g, "")
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
}

/** Strips fenced and inline code so prose checks do not see code. */
export function proseOnly(body) {
  return body.replace(/^```[^\n]*\n[\s\S]*?^```[ \t]*$/gm, "").replace(/`[^`\n]*`/g, "");
}

export function headings(body) {
  const ids = new Set();
  for (const line of body.replace(/^```[^\n]*\n[\s\S]*?^```[ \t]*$/gm, "").split("\n")) {
    const m = line.match(/^#{1,6}\s+(.*?)(?:\s*\[#([A-Za-z0-9_-]+)\])?\s*$/);
    if (m) ids.add(m[2] ?? githubSlug(m[1]));
  }
  return ids;
}

export function fences(body) {
  const out = [];
  const re = /^```([^\n]*)\n([\s\S]*?)^```[ \t]*$/gm;
  let m;
  while ((m = re.exec(body))) {
    const head = m[1].trim();
    const [lang, ...meta] = head.split(/\s+/);
    out.push({ lang: lang ?? "", meta, code: m[2].replace(/\n$/, ""), line: body.slice(0, m.index).split("\n").length });
  }
  return out;
}
