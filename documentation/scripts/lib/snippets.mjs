// Extracts and statically checks the documentation's code. Go snippets and the live replay of curl
// snippets are checked by the Go tests (test/docs_snippets_test.go) against the real handlers.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { REPO, ROOT, CONTENT, fences, parsePage, slugOf } from "./content.mjs";
import { loadSpec, matchOperation, validator } from "./openapi.mjs";

export function allFences() {
  const out = [];
  const walk = (dir) => {
    for (const n of fs.readdirSync(dir).sort()) {
      const f = path.join(dir, n);
      if (fs.statSync(f).isDirectory()) walk(f);
      else if (n.endsWith(".mdx")) for (const fe of fences(parsePage(f).body)) out.push({ ...fe, file: path.relative(ROOT, f), slug: slugOf(f) });
    }
  };
  walk(CONTENT);
  return out;
}

/** Parses the one curl shape the docs use: method, URL, headers and a heredoc or -d body. */
export function parseCurl(code) {
  const flat = code.replace(/\\\n/g, " ");
  const first = flat.split("\n")[0];
  const method = (first.match(/-X\s+([A-Z]+)/) ?? [])[1] ?? (/\s-d\s|\s--data/.test(first) ? "POST" : "GET");
  const url = (first.match(/"(\$NJIWA_MAIL_URL[^"]*)"/) ?? [])[1];
  const headers = [...first.matchAll(/-H\s+"([^"]+)"/g)].map((m) => m[1]);
  let body = null;
  const here = flat.match(/<<'(\w+)'\n([\s\S]*?)\n\1\s*$/);
  if (here) body = here[2];
  else {
    const d = first.match(/-d\s+'([^']*)'/);
    if (d) body = d[1];
  }
  return { method, url, headers, body };
}

export function checkCurl(fe, spec = loadSpec()) {
  const problems = [];
  const c = parseCurl(fe.code);
  const where = `${fe.file}:${fe.line}`;
  if (!c.url) return [`${where}: curl must target "$NJIWA_MAIL_URL/…"`];
  const urlPath = c.url.replace("$NJIWA_MAIL_URL", "").split("?")[0].replace(/\$[A-Z_]+/g, "6f1c2d0e-3b52-4d49-9a43-7d1f0c5e8a11");
  const hit = matchOperation(spec, c.method, urlPath);
  if (!hit) return [`${where}: ${c.method} ${urlPath} is not in openapi.yaml`];
  const secured = hit.op.security === undefined || hit.op.security.length > 0;
  const has = (name) => c.headers.some((h) => h.toLowerCase().startsWith(name.toLowerCase() + ":"));
  if (secured && !c.headers.some((h) => /^Authorization:\s*Bearer\s+\$NJIWA_MAIL_API_KEY$/i.test(h))) problems.push(`${where}: send the key as "Authorization: Bearer $NJIWA_MAIL_API_KEY"`);
  const idem = (hit.op.parameters ?? []).some((p) => p.$ref === "#/components/parameters/IdempotencyKey");
  if (idem && !has("Idempotency-Key")) problems.push(`${where}: ${hit.path} requires an Idempotency-Key header`);
  if (c.body !== null) {
    let json;
    try {
      json = JSON.parse(c.body);
    } catch (e) {
      return [...problems, `${where}: body is not valid JSON (${e.message})`];
    }
    if (!has("Content-Type")) problems.push(`${where}: a JSON body needs a Content-Type header`);
    const schema = hit.op.requestBody?.content?.["application/json"]?.schema;
    if (!schema) problems.push(`${where}: ${hit.path} takes no JSON body`);
    else {
      const validate = validator(spec, schema);
      if (!validate(json)) problems.push(`${where}: body does not match the ${hit.op.operationId} request schema: ${validate.errors.map((e) => `${e.instancePath} ${e.message}`).join("; ")}`);
    }
  }
  return problems;
}

export function checkJson(fe, spec = loadSpec()) {
  const name = (fe.meta.find((m) => m.startsWith("schema=")) ?? "").slice(7);
  let json;
  try {
    json = JSON.parse(fe.code);
  } catch (e) {
    return [`${fe.file}:${fe.line}: invalid JSON (${e.message})`];
  }
  if (!name) return [];
  const validate = validator(spec, name);
  return validate(json) ? [] : [`${fe.file}:${fe.line}: does not match ${name}: ${validate.errors.map((e) => `${e.instancePath} ${e.message}`).join("; ")}`];
}

export function checkTypeScript(frs) {
  if (!frs.length) return [];
  const dir = path.join(ROOT, ".snippet-check", "ts");
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  fs.copyFileSync(path.join(REPO, "clients", "typescript", "index.ts"), path.join(dir, "njiwa-mail.ts"));
  frs.forEach((f, i) => fs.writeFileSync(path.join(dir, `snippet${i}.ts`), f.code + "\n"));
  fs.writeFileSync(
    path.join(dir, "tsconfig.json"),
    JSON.stringify({ compilerOptions: { target: "ES2022", module: "ES2022", moduleResolution: "bundler", strict: true, noEmit: true, skipLibCheck: true, lib: ["ES2022", "DOM"], types: ["node"], typeRoots: [path.join(ROOT, "node_modules", "@types")] }, include: ["*.ts"] }),
  );
  try {
    execFileSync(path.join(ROOT, "node_modules", ".bin", "tsc"), ["-p", dir], { encoding: "utf8", stdio: "pipe" });
    return [];
  } catch (e) {
    return [`TypeScript snippets do not compile:\n${e.stdout ?? e.message}`];
  }
}

export function checkPython(frs) {
  const problems = [];
  for (const f of frs) {
    const tmp = path.join(os.tmpdir(), `snippet-${process.pid}-${f.line}.py`);
    fs.writeFileSync(tmp, f.code + "\n");
    try {
      execFileSync("python3", ["-m", "py_compile", tmp], { stdio: "pipe" });
    } catch (e) {
      problems.push(`${f.file}:${f.line}: Python does not compile: ${e.stderr ?? e.message}`);
    } finally {
      fs.rmSync(tmp, { force: true });
      fs.rmSync(tmp + "c", { force: true });
    }
  }
  return problems;
}
