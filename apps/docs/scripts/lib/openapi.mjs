import fs from "node:fs";
import path from "node:path";
import YAML from "yaml";
import Ajv from "ajv";
import addFormats from "ajv-formats";
import { REPO } from "./content.mjs";

export function loadSpec() {
  return YAML.parse(fs.readFileSync(path.join(REPO, "docs", "openapi.yaml"), "utf8"));
}

/** Resolves $ref and turns the OpenAPI 3.0 schema subset we use into plain JSON Schema. */
export function resolve(spec, node) {
  if (Array.isArray(node)) return node.map((n) => resolve(spec, n));
  if (node && typeof node === "object") {
    if (node.$ref) {
      const target = node.$ref.replace(/^#\//, "").split("/").reduce((o, k) => o?.[k], spec);
      return resolve(spec, target);
    }
    const out = {};
    for (const [k, v] of Object.entries(node)) {
      if (k === "example" || k === "examples" || k.startsWith("x-")) continue;
      out[k] = resolve(spec, v);
    }
    return out;
  }
  return node;
}

export function validator(spec, schemaOrName) {
  const ajv = new Ajv({ allErrors: true, strict: false });
  addFormats(ajv);
  const node = typeof schemaOrName === "string" ? { $ref: `#/components/schemas/${schemaOrName}` } : schemaOrName;
  return ajv.compile(resolve(spec, node));
}

/** Matches a concrete path to an OpenAPI path template. */
export function matchOperation(spec, method, urlPath) {
  for (const [tpl, item] of Object.entries(spec.paths)) {
    const re = new RegExp("^" + tpl.replace(/\{[^}]+\}/g, "[^/]+") + "$");
    if (re.test(urlPath) && item[method.toLowerCase()]) return { path: tpl, op: item[method.toLowerCase()] };
  }
  return null;
}
