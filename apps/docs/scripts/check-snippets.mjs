import { allFences, checkCurl, checkJava, checkJson, checkPhp, checkPython, checkTypeScript } from "./lib/snippets.mjs";
import { loadSpec } from "./lib/openapi.mjs";

const spec = loadSpec();
const all = allFences();
const problems = [];
const checked = { curl: 0, ts: 0, python: 0, php: 0, java: 0, json: 0 };
for (const f of all) {
  if (f.meta.includes("curl-check")) (checked.curl++, problems.push(...checkCurl(f, spec)));
  if (f.meta.includes("check") && f.lang === "json") (checked.json++, problems.push(...checkJson(f, spec)));
}
const ts = all.filter((f) => f.meta.includes("check") && f.lang === "ts");
const py = all.filter((f) => f.meta.includes("check") && f.lang === "python");
checked.ts = ts.length;
checked.python = py.length;
const php = all.filter((f) => f.meta.includes("check") && f.lang === "php");
checked.php = php.length;
const phpResult = checkPhp(php);
const java = all.filter((f) => f.meta.includes("check") && f.lang === "java");
checked.java = java.length;
const javaResult = checkJava(java);
problems.push(...checkTypeScript(ts), ...checkPython(py), ...phpResult.problems, ...javaResult.problems);
if (problems.length) {
  console.error(problems.join("\n"));
  process.exit(1);
}
console.log(`snippets ok: ${JSON.stringify(checked)}${phpResult.skipped ? " (PHP NOT checked: php not installed)" : ""}${javaResult.skipped ? " (Java NOT checked: javac or JAVA_SNIPPET_CLASSPATH missing)" : ""} (Go snippets and the live curl replay run in the Go tests)`);
