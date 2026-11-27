import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Light mode accessibility, computed from the tokens themselves: body text at
// least 4.5:1, control outlines and the focus ring at least 3:1.
const read = (p: string) => readFileSync(resolve(process.cwd(), p), "utf8");
const tokens = read("src/styles/opendashboards/tokens.css");
const welcome = read("src/features/welcome/welcome.css");

function block(css: string, selector: string): Record<string, string> {
  // The first rule that starts a line with exactly this selector.
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const from = new RegExp(`^${escaped} \\{`, "m").exec(css)?.index;
  if (from === undefined) throw new Error(`no ${selector}`);
  const body = css.slice(from, css.indexOf("\n}", from));
  return Object.fromEntries(
    [...body.matchAll(/(--[a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]),
  );
}

type RGB = [number, number, number, number];
function parse(value: string): RGB {
  const hex = /^#([0-9a-f]{6})$/i.exec(value);
  if (hex) {
    const n = parseInt(hex[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255, 1];
  }
  const rgb = /^rgba?\(([^)]+)\)$/.exec(value);
  if (rgb) {
    const p = rgb[1]
      .split(/[\s,/]+/)
      .filter(Boolean)
      .map(Number);
    return [p[0], p[1], p[2], p[3] ?? 1];
  }
  throw new Error(`cannot read colour ${value}`);
}
const over = (top: RGB, under: RGB, alpha = top[3]): RGB => [
  top[0] * alpha + under[0] * (1 - alpha),
  top[1] * alpha + under[1] * (1 - alpha),
  top[2] * alpha + under[2] * (1 - alpha),
  1,
];
function luminance([r, g, b]: RGB) {
  const c = [r, g, b].map((v) => {
    const x = v / 255;
    return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}
function ratio(fg: RGB, bg: RGB) {
  const [a, b] = [luminance(over(fg, bg)), luminance(bg)].sort((x, y) => y - x);
  return (a + 0.05) / (b + 0.05);
}

const dash = block(tokens, ".od");
const app = block(tokens, ".od-app");
const mkt = block(tokens, ".od-mkt");
const page = block(welcome, ".welcome-page");

describe("light palette", () => {
  const c = (v: Record<string, string>, k: string) => parse(v[k]);
  const text: [string, RGB, RGB][] = [];
  for (const bg of ["--od-app-background", "--od-app-card", "--od-app-muted", "--od-app-sidebar"]) {
    text.push([`foreground on ${bg}`, c(app, "--od-app-foreground"), c(app, bg)]);
    text.push([`muted text on ${bg}`, c(app, "--od-app-muted-foreground"), c(app, bg)]);
  }
  text.push(
    ["button", c(app, "--od-app-primary-foreground"), c(app, "--od-app-primary")],
    ["secondary", c(app, "--od-app-secondary-foreground"), c(app, "--od-app-secondary")],
    ["destructive button", parse("#fbfbfb"), c(app, "--od-app-destructive")],
  );
  for (const bg of ["--od-page", "--od-surface"])
    for (const fg of ["--od-ink", "--od-ink-secondary", "--od-ink-muted"])
      text.push([`${fg} on ${bg}`, c(dash, fg), c(dash, bg)]);
  // Status chips: the colour as text on a 15% tint of itself, over a card.
  for (const k of ["--od-good", "--od-warning", "--od-bad", "--od-info"]) {
    const card = c(dash, "--od-surface");
    text.push([`${k} chip`, c(dash, k), over(c(dash, k), card, 0.15)]);
    text.push([`${k} on page`, c(dash, k), c(dash, "--od-page")]);
  }
  text.push(
    ["landing text", c(mkt, "--od-mkt-ink"), c(mkt, "--od-mkt-page")],
    ["landing muted", c(mkt, "--od-mkt-muted"), c(mkt, "--od-mkt-page")],
    ["welcome text", c(page, "--text"), c(page, "--bg")],
    ["welcome muted", c(page, "--text-muted"), c(page, "--bg")],
    ["welcome link", c(page, "--accent"), c(page, "--bg")],
    ["welcome button", c(page, "--accent-ink"), c(page, "--accent")],
  );

  it.each(text)("%s is at least 4.5:1", (_name, fg, bg) => {
    expect(ratio(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });

  it.each(["--od-app-input", "--od-app-ring"])("%s is at least 3:1", (k) => {
    for (const bg of ["--od-app-background", "--od-app-card"])
      expect(ratio(c(app, k), c(app, bg))).toBeGreaterThanOrEqual(3);
  });

  it("is soft: no pure white or black, and cards lighter than the page", () => {
    for (const v of [app["--od-app-background"], app["--od-app-card"], dash["--od-surface"]])
      expect(v).not.toMatch(/^#fff(fff)?$|oklch\(1 0 0\)/i);
    expect(app["--od-app-foreground"]).not.toMatch(/^#000/);
    expect(luminance(c(app, "--od-app-card"))).toBeGreaterThan(
      luminance(c(app, "--od-app-background")),
    );
  });

  it("leaves dark mode as it was", () => {
    const dark = block(tokens, '.od-app[data-mode="dark"]');
    expect(dark["--od-app-background"]).toBe("oklch(0.145 0 0)");
    expect(dark["--od-app-foreground"]).toBe("oklch(0.985 0 0)");
    expect(dark["--od-app-input"]).toBe(dark["--od-app-border"]);
    const od = block(tokens, '.od[data-mode="dark"]');
    expect([od["--od-page"], od["--od-ink"], od["--od-good"]]).toEqual([
      "#141414",
      "#fafafa",
      "#7fe0b1",
    ]);
  });
});
