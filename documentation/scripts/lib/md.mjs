// MDX -> plain Markdown for agents and for "Copy Markdown". Components become plain Markdown;
// internal links become absolute .md links so a fetched page is self-contained.

export function mdxToMarkdown(body, baseUrl) {
  const lines = body.replace(/\r\n/g, "\n").split("\n");
  const out = [];
  let fence = false;
  let callout = null; // { prefix }
  for (let line of lines) {
    if (/^```/.test(line)) fence = !fence;
    if (fence || /^```/.test(line)) {
      out.push(callout ? "> " + line : line);
      continue;
    }
    const t = line.trim();
    let m;
    if ((m = t.match(/^<Callout([^>]*)>$/))) {
      const title = (m[1].match(/title="([^"]*)"/) ?? [])[1];
      const type = (m[1].match(/type="([^"]*)"/) ?? [])[1];
      callout = {};
      out.push(`> **${title ?? (type === "warn" ? "Warning" : "Note")}**`);
      out.push(">");
      continue;
    }
    if (/^<Callout[^>]*>/.test(t) && /<\/Callout>$/.test(t)) {
      out.push("> " + t.replace(/<\/?Callout[^>]*>/g, "").trim());
      continue;
    }
    if (t === "</Callout>") {
      callout = null;
      out.push("");
      continue;
    }
    if ((m = t.match(/^<Tab value="([^"]*)">$/))) {
      out.push(`**${m[1]}**`, "");
      continue;
    }
    if (/^<\/?(Tabs|Tab|Steps|Step|Cards|Card)\b[^>]*>$/.test(t)) continue;
    out.push(callout ? (line.trim() ? "> " + line : ">") : line);
  }
  let md = out.join("\n").replace(/\n{3,}/g, "\n\n").trim() + "\n";
  md = md.replace(/\]\((\/docs(?:\/[A-Za-z0-9._\/-]*)?)(#[^)]*)?\)/g, (_, p, h = "") => `](${baseUrl}${p === "/docs" ? "/docs/index" : p}.md${h})`);
  md = md.replace(/\]\((\/(?:llms\.txt|llms-full\.txt|openapi\.yaml|openapi\.json|skills\/[^)]*))\)/g, (_, p) => `](${baseUrl}${p})`);
  return md;
}
