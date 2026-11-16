// A minimal static server for browser tests. It mirrors deploy/nginx.conf routing:
// /x -> x.html, /x/ -> x/index.html, content types for .md/.txt/.yaml/.json, 404.html with status 404.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";

const root = path.resolve(process.argv[2] ?? "out");
const port = Number(process.argv[3] ?? 4173);
const types = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".md": "text/markdown; charset=utf-8", ".txt": "text/plain; charset=utf-8", ".yaml": "application/yaml", ".svg": "image/svg+xml", ".ico": "image/x-icon", ".woff2": "font/woff2" };

function resolveFile(url) {
  const clean = decodeURIComponent(url.split("?")[0]);
  if (clean.includes("..")) return null;
  const rel = clean.replace(/^\/+/, "");
  for (const c of [rel, rel + ".html", path.join(rel, "index.html")]) {
    const f = path.join(root, c);
    if (f.startsWith(root) && fs.existsSync(f) && fs.statSync(f).isFile()) return f;
  }
  return null;
}

http
  .createServer((req, res) => {
    const f = resolveFile(req.url ?? "/");
    const file = f ?? path.join(root, "404.html");
    const status = f ? 200 : 404;
    res.writeHead(status, { "Content-Type": types[path.extname(file)] ?? (path.basename(file) === "search" ? "application/json" : "application/octet-stream"), "X-Content-Type-Options": "nosniff" });
    fs.createReadStream(file).pipe(res);
  })
  .listen(port, "0.0.0.0", () => console.log(`serving ${root} on :${port}`));
