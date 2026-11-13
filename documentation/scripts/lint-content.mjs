import { lintAll } from "./lib/lint.mjs";

const problems = lintAll();
if (problems.length) {
  console.error(problems.join("\n"));
  console.error(`\n${problems.length} content problem(s).`);
  process.exit(1);
}
console.log("content lint: ok");
