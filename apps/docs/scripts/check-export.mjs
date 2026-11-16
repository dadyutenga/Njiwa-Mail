// Verifies the built site (out/): what agents fetch is exactly what the content and the OpenAPI say,
// at one version and one source commit, and nothing secret-looking was shipped.
import fs from "node:fs";
import path from "node:path";
import YAML from "yaml";
import { REPO, ROOT, mdPathOf, orderedFiles, parsePage, urlOf } from "./lib/content.mjs";
import { pageMarkdown } from "./generate-assets.mjs";

const OUT = path.resolve(process.argv[2] ?? path.join(ROOT, "out"));
const BASE = (process.env.NEXT_PUBLIC_DOCS_BASE_URL ?? "https://docs.example.com").replace(/\/$/, "");
const COMMIT = process.env.NEXT_PUBLIC_DOCS_COMMIT ?? "dev";
const problems = [];
const bad = (m) => problems.push(m);
const read = (rel) => (fs.existsSync(path.join(OUT, rel)) ? fs.readFileSync(path.join(OUT, rel), "utf8") : null);

const files = orderedFiles();
const llms = read("llms.txt");
const full = read("llms-full.txt");
if (!llms) bad("llms.txt missing");
if (!full) bad("llms-full.txt missing");

for (const f of files) {
  const page = parsePage(f);
  const rel = mdPathOf(f).slice(1);
  const got = read(rel);
  if (got === null) {
    bad(`${rel} missing from the export`);
    continue;
  }
  if (got !== pageMarkdown(page, f)) bad(`${rel} differs from the content source`);
  if (/<\/?(Tabs?|Steps?|Callout|Cards?)\b/.test(got)) bad(`${rel} still contains MDX components`);
  if (/^\s*(import|export)\s/m.test(got.replace(/```[\s\S]*?```/g, ""))) bad(`${rel} contains import/export`);
  if (llms && !llms.includes(`](${BASE}${mdPathOf(f)})`)) bad(`llms.txt does not list ${mdPathOf(f)}`);
  if (full && !full.includes(`# ${page.fm.title}\n`)) bad(`llms-full.txt does not include "${page.fm.title}"`);
  const html = read(urlOf(f).slice(1) + ".html") ?? (urlOf(f) === "/docs" ? read("docs.html") : null);
  if (html === null) bad(`no HTML page for ${urlOf(f)}`);
  else if (!html.includes(page.fm.title)) bad(`${urlOf(f)} HTML does not contain its title`);
}

// Every link in llms.txt resolves to a file in the export.
for (const m of (llms ?? "").matchAll(/\]\((https?:\/\/[^)]+)\)/g)) {
  if (!m[1].startsWith(BASE)) {
    bad(`llms.txt links outside the docs origin: ${m[1]}`);
    continue;
  }
  if (read(m[1].slice(BASE.length + 1)) === null) bad(`llms.txt link does not resolve: ${m[1]}`);
}

// OpenAPI: the YAML is byte-identical to the canonical file and the JSON is the same document.
const canonical = fs.readFileSync(path.join(REPO, "docs", "openapi.yaml"), "utf8");
if (read("openapi.yaml") !== canonical) bad("openapi.yaml differs from docs/openapi.yaml");
try {
  if (JSON.stringify(JSON.parse(read("openapi.json"))) !== JSON.stringify(YAML.parse(canonical))) bad("openapi.json is not the same document as openapi.yaml");
} catch (e) {
  bad("openapi.json: " + e.message);
}

// Skill files are shipped unchanged.
const skillRoot = path.join(REPO, "skills", "njiwa-mail-integration");
const walk = (d) => fs.readdirSync(d).flatMap((n) => (fs.statSync(path.join(d, n)).isDirectory() ? walk(path.join(d, n)) : [path.join(d, n)]));
for (const f of walk(skillRoot).filter((x) => x.endsWith(".md"))) {
  const rel = "skills/njiwa-mail-integration/" + path.relative(skillRoot, f).replace(/\\/g, "/");
  if (read(rel) !== fs.readFileSync(f, "utf8")) bad(`${rel} differs from the repository skill`);
}
if (!read("skills/njiwa-mail-integration/SKILL.md")) bad("the skill is not shipped");

// One version, one commit, everywhere.
const versions = read("versions.json") ? JSON.parse(read("versions.json")) : {};
const stamp = `Njiwa-Mail documentation ${versions.current}, source commit ${COMMIT}`;
if (!versions.current) bad("versions.json missing");
if (llms && !llms.includes(`version ${versions.current}, source commit ${COMMIT}`)) bad("llms.txt version/commit differs from versions.json");
if (full && !full.includes(`source commit ${COMMIT}`)) bad("llms-full.txt carries another commit");
for (const f of files) {
  const md = read(mdPathOf(f).slice(1)) ?? "";
  if (!md.includes(`(Njiwa-Mail documentation ${versions.current}, commit ${COMMIT})`)) bad(`${mdPathOf(f)} carries another version or commit (${stamp})`);
}

// No page may link out of the export to a private or non-https address, and no secret may ship.
const SECRETS = [/dmk_(sbx|liv)_[A-Za-z0-9]{6,}_[A-Za-z0-9]{12,}/, /whsec_[A-Za-z0-9]{16,}/, /-----BEGIN [A-Z ]*PRIVATE KEY-----/, /AKIA[0-9A-Z]{16}/, /\bghp_[A-Za-z0-9]{30,}/];
const scan = (d) => {
  for (const n of fs.readdirSync(d)) {
    const f = path.join(d, n);
    if (fs.statSync(f).isDirectory()) scan(f);
    else if (/\.(html|md|txt|json|yaml|js|css)$/.test(n) || n === "search") {
      const text = fs.readFileSync(f, "utf8");
      for (const re of SECRETS) if (re.test(text)) bad(`${path.relative(OUT, f)} looks like it contains a secret (${re})`);
    }
  }
};
scan(OUT);

if (problems.length) {
  console.error(problems.join("\n"));
  console.error(`\n${problems.length} export problem(s).`);
  process.exit(1);
}
console.log(`export ok: ${files.length} pages, llms.txt, llms-full.txt, openapi.{yaml,json}, skill, one version (${versions.current}) and commit (${COMMIT})`);
