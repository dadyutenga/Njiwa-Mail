/** Template variables as typed in the dashboard (always text) to the values the API expects. */

export type VarType = "string" | "int" | "url" | "fields";

/** One "Label: value" per line becomes a details row. A line without a colon is a label alone. */
export function parseFieldLines(text: string) {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const i = line.indexOf(":");
      return i < 0
        ? { label: line, value: "" }
        : { label: line.slice(0, i).trim(), value: line.slice(i + 1).trim() };
    });
}

export function variableValue(type: VarType, raw: string): unknown {
  if (type === "int") return Number(raw);
  if (type === "fields") return parseFieldLines(raw);
  return raw;
}

/** Row keys inside {{range .details}}…{{end}}; they are not template variables. */
export const FIELD_ROW_KEYS = new Set(["label", "value"]);
