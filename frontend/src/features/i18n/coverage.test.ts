import { expect, it } from "vitest";
import { swExtra } from "./sw-extra";
import { swPhrases } from "./i18n";

// Every literal passed to phrase() must have a Kiswahili translation, so a new
// screen cannot ship half-translated. Dynamic strings (template literals with
// ${…}) are translated by the screens that build them.
const files = import.meta.glob("../../**/*.tsx", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

it("has a Kiswahili translation for every static phrase()", () => {
  const re = /phrase\(\s*("(?:[^"\\]|\\.)*")\s*[,)]/g;
  const missing = new Set<string>();
  for (const [path, src] of Object.entries(files)) {
    if (/\.test\.tsx?$/.test(path) || path.includes("/i18n/")) continue;
    for (const m of src.matchAll(re)) {
      const text = JSON.parse(m[1]) as string;
      if (!(text in swPhrases) && !(text in swExtra)) missing.add(text);
    }
  }
  expect([...missing].sort()).toEqual([]);
});

it("does not leave an English sentence as its own translation", () => {
  const same = Object.entries(swExtra).filter(
    ([en, sw]) => en === sw && /[a-z]{4,} [a-z]{4,} [a-z]{4,}/.test(en),
  );
  expect(same).toEqual([]);
});
