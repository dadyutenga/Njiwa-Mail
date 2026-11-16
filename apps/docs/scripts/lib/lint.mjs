// Content rules. MDX is code: a malicious or careless PR must not be able to run script in the
// published site, make a claim the product cannot keep, or leak a secret.
import fs from "node:fs";
import path from "node:path";
import { CONTENT, fences, githubSlug, headings, parsePage, proseOnly, slugOf } from "./content.mjs";

export const COMPONENTS = new Set(["Tabs", "Tab", "Steps", "Step", "Callout", "Cards", "Card"]);
const STATIC_PATHS = [/^\/$/, /^\/llms(-full)?\.txt$/, /^\/openapi\.(yaml|json)$/, /^\/skills\/njiwa-mail-integration\/.+\.md$/];
const FENCE_LANGS = new Set(["bash", "sh", "http", "json", "ts", "go", "python", "yaml", "text", "ini", "dotenv", "diff", "sql", "html"]);

const SECRET_PATTERNS = [
  [/dmk_(sbx|liv)_[A-Za-z0-9]{6,}_[A-Za-z0-9]{12,}/, "an API key"],
  [/whsec_[A-Za-z0-9]{16,}/, "a webhook secret"],
  [/-----BEGIN [A-Z ]*PRIVATE KEY-----/, "a private key"],
  [/AKIA[0-9A-Z]{16}/, "an AWS access key"],
  [/\bsk-[A-Za-z0-9]{20,}/, "an API secret"],
  [/\bghp_[A-Za-z0-9]{30,}/, "a GitHub token"],
  [/\bxox[abp]-[A-Za-z0-9-]{10,}/, "a Slack token"],
  [/\bAuthorization:\s*Bearer\s+(?!\$|<|dmk_(sbx|liv)_\.\.\.)[A-Za-z0-9._-]{20,}/, "a real bearer token"],
];

export function allSlugs() {
  const out = new Map();
  const walk = (dir) => {
    for (const n of fs.readdirSync(dir)) {
      const f = path.join(dir, n);
      if (fs.statSync(f).isDirectory()) walk(f);
      else if (n.endsWith(".mdx")) out.set("/docs" + (slugOf(f) ? "/" + slugOf(f) : ""), headings(parsePage(f).body));
    }
  };
  walk(CONTENT);
  return out;
}

/** @returns {string[]} problems for one page. */
export function lintPage(file, slugs = allSlugs()) {
  const rel = path.relative(path.dirname(CONTENT), file);
  const page = parsePage(file);
  const problems = [];
  const bad = (msg) => problems.push(`${rel}: ${msg}`);

  if (!page.fm) return [`${rel}: missing or unreadable frontmatter`];
  if (typeof page.fm.title !== "string" || page.fm.title.length < 3) bad("frontmatter needs a title");
  if (typeof page.fm.description !== "string" || page.fm.description.length < 10) bad("frontmatter needs a description");
  if (page.fm.status !== undefined && !["stable", "planned"].includes(page.fm.status)) bad("status must be stable or planned");

  const prose = proseOnly(page.body);

  // --- executable content
  for (const [i, line] of prose.split("\n").entries()) {
    if (/^\s*(import|export)\s/.test(line)) bad(`line ${i + 1}: import/export is not allowed in content`);
  }
  const braces = prose.replace(/items=\{\[[^\]{}]*\]\}/g, "");
  if (/[{}]/.test(braces)) bad("curly braces outside code are JavaScript expressions in MDX; put the text in `code` or a fence");
  for (const m of prose.matchAll(/<\/?\s*([A-Za-z][A-Za-z0-9]*)/g)) {
    if (!COMPONENTS.has(m[1])) bad(`element <${m[1]}> is not allowed (allowed: ${[...COMPONENTS].join(", ")})`);
  }
  if (/\son[a-z]+\s*=/i.test(prose)) bad("event-handler attributes are not allowed");
  if (/dangerouslySetInnerHTML/.test(page.body)) bad("dangerouslySetInnerHTML is not allowed");

  // --- links
  for (const m of prose.matchAll(/\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
    const url = m[1];
    if (/^(javascript|data|vbscript|file):/i.test(url)) bad(`unsafe link ${url}`);
    else if (/^http:\/\//i.test(url)) bad(`link ${url} must be https`);
    else if (url.startsWith("//")) bad(`protocol-relative link ${url}`);
    else if (/^https:\/\//.test(url)) continue;
    else if (url.startsWith("/")) {
      const [p, hash] = url.split("#");
      if (STATIC_PATHS.some((re) => re.test(p))) continue;
      const ids = slugs.get(p.replace(/\/$/, ""));
      if (!ids) bad(`broken internal link ${url}`);
      else if (hash && !ids.has(hash)) bad(`link ${url}: no heading "${hash}" on that page`);
    } else if (url.startsWith("#")) {
      if (!headings(page.body).has(url.slice(1))) bad(`broken anchor ${url}`);
    } else bad(`relative link ${url}: use an absolute /docs/... path`);
  }

  // --- claims the product cannot keep
  for (const [i, line] of prose.split("\n").entries()) {
    if (/\bdelivered\b|\bdelivery is guaranteed\b|\bguaranteed delivery\b/i.test(line) && !/\b(not|never|no|cannot|can't|isn't|without|nor|any)\b/i.test(line))
      bad(`line ${i + 1}: do not claim delivery ("${line.trim().slice(0, 60)}…")`);
    if (/(requires?|must use|must have|needs?|only works with)\s+mailcow/i.test(line)) bad(`line ${i + 1}: Mailcow is optional; do not require it`);
  }
  if (/ditronics/i.test(page.raw)) bad("the Ditronics name must not appear");
  if (/njiwa\.co\.tz/i.test(page.raw)) bad("use example.com placeholders, not a real or hosted endpoint");
  if (/\bhttps?:\/\/(localhost|127\.0\.0\.1)/i.test(page.raw)) bad("examples must use the https://mail-api.example.com placeholder (or $NJIWA_MAIL_URL)");

  // --- secrets
  for (const [re, what] of SECRET_PATTERNS) if (re.test(page.raw)) bad(`looks like ${what}; use a placeholder`);

  // --- code fences
  for (const f of fences(page.body)) {
    if (!f.lang) bad(`code fence at line ${f.line} has no language`);
    else if (!FENCE_LANGS.has(f.lang)) bad(`code fence at line ${f.line}: unknown language "${f.lang}"`);
    const checkTag = f.meta.includes("check") || f.meta.includes("curl-check");
    if (f.meta.includes("curl-check") && !(f.lang === "bash" && /^curl\b/.test(f.code.trim()))) bad(`curl-check fence at line ${f.line} must be a bash curl command`);
    if (f.meta.includes("check") && !["ts", "go", "python", "json"].includes(f.lang)) bad(`check fence at line ${f.line}: only ts, go, python and json can be checked`);
    if (checkTag && f.code.length > 6000) bad(`checked fence at line ${f.line} is too long`);
  }
  return problems;
}

export function lintAll() {
  const slugs = allSlugs();
  const problems = [];
  const walk = (dir) => {
    for (const n of fs.readdirSync(dir)) {
      const f = path.join(dir, n);
      if (fs.statSync(f).isDirectory()) walk(f);
      else if (n.endsWith(".mdx")) problems.push(...lintPage(f, slugs));
    }
  };
  walk(CONTENT);
  // Every meta.json entry must exist.
  const walkMeta = (dir) => {
    const meta = path.join(dir, "meta.json");
    if (fs.existsSync(meta)) {
      for (const e of JSON.parse(fs.readFileSync(meta, "utf8")).pages ?? []) {
        if (/^---.*---$/.test(e)) continue;
        const name = e.replace(/^\.\.\./, "");
        if (!fs.existsSync(path.join(dir, name)) && !fs.existsSync(path.join(dir, name + ".mdx"))) problems.push(`${path.relative(CONTENT, meta)}: "${e}" does not exist`);
      }
    }
    for (const n of fs.readdirSync(dir)) if (fs.statSync(path.join(dir, n)).isDirectory()) walkMeta(path.join(dir, n));
  };
  walkMeta(CONTENT);
  return problems;
}

export { githubSlug };
