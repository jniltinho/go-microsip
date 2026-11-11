// The skin contract (openspec ui-themes, design D7): every skin defines the same tokens, colors live
// only in this folder, corners are rounded only by the radius tokens and only in the skins of
// systems that round them, and every skin meets WCAG AAA: text at 7:1, the edges of controls, state
// icons and the focus ring at 3:1, a solid focus ring of at least 2 px.
import { expect, test } from "vite-plus/test";
import { effectiveSkin, knownSkin, SKINS } from "./index";

const sheets = import.meta.glob<string>("./*.css", {
  query: "?raw",
  import: "default",
  eager: true,
});
const sources = import.meta.glob<string>(
  ["../**/*.{vue,ts,css}", "!../themes/**", "!../**/*.test.ts"],
  {
    query: "?raw",
    import: "default",
    eager: true,
  },
);

/** The body of a skin's `:root[data-theme]` block. */
const block = (id: string) =>
  new RegExp(`:root\\[data-theme="${id}"\\]\\s*\\{([^}]*)\\}`).exec(
    sheets[`./${id}.css`] ?? "",
  )?.[1] ?? "";
const DECLARATION = /(--[\w-]+)\s*:\s*([^;]+);/g;

/** The tokens of one skin file, by name. */
function tokens(id: string): Map<string, string> {
  return new Map([...block(id).matchAll(DECLARATION)].map((m) => [m[1], m[2].trim()]));
}

test("the seven shipped skins are registered, each with its own stylesheet", () => {
  expect(SKINS.map((s) => s.id)).toEqual([
    "light",
    "dark",
    "xp",
    "macos",
    "ubuntu",
    "win98",
    "matrix",
  ]);
  expect(Object.keys(sheets).sort()).toEqual(
    ["./index.css", ...SKINS.map((s) => `./${s.id}.css`)].sort(),
  );
  for (const { id } of SKINS) {
    expect(sheets["./index.css"], `index.css imports ${id}`).toContain(`@import "./${id}.css";`);
    expect(tokens(id).size, `${id} has a :root[data-theme="${id}"] block`).toBeGreaterThan(0);
    // Nothing but tokens and the color scheme: a stray line would make the whole sheet invalid.
    const rest = block(id)
      .replace(DECLARATION, "")
      .replace(/color-scheme:\s*(light|dark);/, "");
    expect(rest.trim(), `${id}: text that is not a token declaration`).toBe("");
  }
});

test("every skin defines the same token set", () => {
  const reference = [...tokens("light").keys()].sort();
  for (const { id } of SKINS) {
    const own = [...tokens(id).keys()].sort();
    expect(
      {
        skin: id,
        missing: reference.filter((t) => !own.includes(t)),
        extra: own.filter((t) => !reference.includes(t)),
      },
      `skin ${id}`,
    ).toEqual({ skin: id, missing: [], extra: [] });
  }
});

// Corners are square unless the system a skin evokes rounds them (design D7, changed on 2026-11-11).
const ROUNDED = ["xp", "macos", "ubuntu"];
const RADII = ["--radius", "--radius-window", "--radius-menu", "--tab-radius", "--close-radius"];

test("corners are rounded only by the radius tokens, and only in the skins of rounded systems", () => {
  for (const { id } of SKINS) {
    if (ROUNDED.includes(id)) continue;
    for (const token of [...RADII, "--scroll-radius"])
      expect(tokens(id).get(token), `${id} ${token}`).toBe("0");
  }
  for (const id of ROUNDED) expect(tokens(id).get("--radius"), id).toMatch(/^[1-9]\d*px$/);
  for (const [file, text] of Object.entries(sources)) {
    expect(text, file).not.toMatch(/\brounded(-|\b)/);
    // A radius comes from a token (a check box takes half of one); only a radio is a disc everywhere.
    const allowed =
      /border-radius: (var\(--[\w-]+\)|calc\(var\(--radius\) \/ 2\)|50%; \/\* a radio button is a disc in every system \*\/)/g;
    expect(text.replace(allowed, ""), file).not.toContain("border-radius");
  }
});

test("nothing moves for longer than five seconds, and nothing at all with reduced motion", () => {
  for (const { id } of SKINS) {
    const motion = tokens(id).get("--backdrop-motion") ?? "";
    if (motion === "none") continue;
    // One run of a named animation, shorter than the five seconds of WCAG 2.2.2.
    const m = /^backdrop (\d+(?:\.\d+)?)s [\w-]+ 1$/.exec(motion);
    expect(m, `${id}: --backdrop-motion "${motion}"`).not.toBeNull();
    expect(Number(m?.[1]), id).toBeLessThan(5);
  }
  const style = sources["../style.css"];
  expect(style).toMatch(
    /@media \(prefers-reduced-motion: reduce\) \{[^}]*\{\s*animation: none !important;\s*transition: none !important;/,
  );
  for (const [file, text] of Object.entries(sources))
    if (file !== "../style.css") expect(text, file).not.toMatch(/animation|transition|@keyframes/);
});

test("system resolves to light or dark and unknown ids resolve to system", () => {
  expect([effectiveSkin("system", false).id, effectiveSkin("system", true).id]).toEqual([
    "light",
    "dark",
  ]);
  expect(effectiveSkin("win98", true).id).toBe("win98");
  expect([knownSkin("xp"), knownSkin("removed-skin"), knownSkin(null)]).toEqual([
    "xp",
    "system",
    "system",
  ]);
  expect(effectiveSkin(knownSkin("removed-skin"), true).id).toBe("dark");
});

test("no color literal outside src/themes", () => {
  const palette =
    "white|black|slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose";
  const literals = [
    /#[0-9a-fA-F]{3,8}\b/,
    /\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(/,
    new RegExp(
      `\\b(bg|text|border|outline|ring|fill|stroke|from|via|to|accent|caret|divide|shadow|decoration)-(${palette})(-\\d+)?\\b`,
    ),
    new RegExp(`(color|background|border|fill|stroke)[\\w-]*\\s*:\\s*(${palette})\\b`),
  ];
  expect(Object.keys(sources).length).toBeGreaterThan(5);
  for (const [file, text] of Object.entries(sources))
    text.split("\n").forEach((line, i) => {
      for (const literal of literals) expect(line, `${file}:${i + 1}`).not.toMatch(literal);
    });
});

test("no source renders dynamic text as HTML", () => {
  for (const [file, text] of Object.entries(sources))
    expect(text, file).not.toMatch(/v-html|innerHTML|outerHTML/);
});

test("only api.ts reaches the backend", () => {
  for (const [file, text] of Object.entries(sources)) {
    if (file.endsWith("/api.ts")) continue;
    expect(text, file).not.toMatch(/@wailsio\/runtime/);
    expect(text, file).not.toMatch(/from "\.\.?\/mock"|import\("\.\.?\/mock"\)/);
  }
});

// WCAG relative luminance and contrast ratio of two #rrggbb colors (the only form the skins use).
function luminance(hex: string): number {
  const h = hex.slice(1);
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(h.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}
/** Every color of a token value: a gradient is checked at each of its stops. */
const colors = (value: string): string[] => value.match(/#[0-9a-fA-F]{6}\b/g) ?? [];

/** The colors of a token. The surface also shows the skin's backdrop, so its colors count there. */
function colorsOf(t: Map<string, string>, name: string): string[] {
  const own = colors(t.get(`--${name}`) ?? "");
  return name === "surface" ? [...own, ...colors(t.get("--backdrop") ?? "")] : own;
}
/**
 * The contrast of foreground tokens on background tokens: against a background, a color counts by
 * its worst stop; of several foreground colors (an edge drawn by a border and a bevel), the best
 * one counts when `best` is set. Undefined when a side holds no color (a clear background).
 */
function ratio(t: Map<string, string>, fgs: string[], bgs: string[], best = false) {
  const bg = bgs.flatMap((n) => colorsOf(t, n));
  const each = fgs
    .flatMap((n) => colorsOf(t, n))
    .map((f) => Math.min(...bg.map((b) => contrast(f, b))));
  if (!each.length || !bg.length) return undefined;
  return best ? Math.max(...each) : Math.min(...each);
}

const STATES = ["active", "ringing", "held", "muted", "error", "secure", "insecure"];
/** What a control or a text may stand on besides a well: the window and a group box. */
const AROUND = ["surface", "panel", "group"];
/** Text token on background token, as the components pair them (style.css): 7:1. */
const TEXT_PAIRS: [string, string][] = [
  ...["surface", "panel", "input", "hover", "group"].map((bg): [string, string] => ["text", bg]),
  ...["surface", "panel", "input", "hover", "group", "btn", "btn-hover", "btn-active"].map(
    (bg): [string, string] => ["text-muted", bg],
  ),
  ["link", "surface"],
  ["link", "panel"],
  ["selection-text", "selection"],
  ...["btn-text", "key-text", "key-sub"].flatMap((fg): [string, string][] => [
    [fg, fg === "btn-text" ? "btn" : "key"],
    [fg, "btn-hover"],
    [fg, "btn-active"],
  ]),
  ["call-text", "call"],
  ["hangup-text", "hangup"],
  ["notice-text", "notice"],
  ["state-error", "notice"],
  ["title-text", "title"],
  ["tab-text", "tab"],
  ["tab-text", "tab-bar"],
  ["tab-active-text", "tab-active"],
  ["status-text", "status"],
  ["group-title", "surface"],
  ["group-title", "group"],
  ...STATES.flatMap((s) => AROUND.map((bg): [string, string] => [`state-${s}`, bg])),
];
/**
 * What is not text, at 3:1 (WCAG 1.4.11): name, the tokens that draw it (the best one counts) and
 * what it stands on. A button's edge is its border or its bevel; a check box and a radio are small
 * fields whose mark is drawn in --accent-text on --accent; the selection fill is what shows the focused menu item; the pressed bevel is what tells a
 * pressed toggle from a released one; the focus ring is drawn on the window, a well or a group and
 * beside the plain faces (in a title bar, the tab bar or a selected row it takes that place's text
 * color, which the text pairs hold at 7:1).
 */
const UI_PAIRS: [string, string[], string][] = [
  ["check mark", ["accent-text"], "accent"],
  ["selection fill", ["selection"], "panel"],
  ...[...AROUND, "title", "tab-bar"].map((bg): [string, string[], string] => [
    "button edge",
    ["btn-border", "btn-bevel"],
    bg,
  ]),
  ...[...AROUND, "input"].map((bg): [string, string[], string] => [
    "field edge",
    ["field-border", "field-bevel"],
    bg,
  ]),
  ["pressed button", ["btn-bevel-pressed"], "btn-active"],
  ...[...AROUND, "input", "btn", "key"].map((bg): [string, string[], string] => [
    "focus ring",
    ["focus"],
    bg,
  ]),
  ["close icon", ["close-text"], "close"],
  ...["active", "ringing", "error"].flatMap((s) =>
    ["status", "hover"].map((bg): [string, string[], string] => [
      `state icon ${s}`,
      [`state-${s}`],
      bg,
    ]),
  ),
];

test("text meets 7:1 on its background in every skin", () => {
  expect(contrast("#000000", "#ffffff")).toBeCloseTo(21);
  const rows = SKINS.flatMap(({ id }) =>
    TEXT_PAIRS.map(([fg, bg]) => ({ skin: id, fg, bg, ratio: ratio(tokens(id), [fg], [bg]) })),
  ).filter((r) => r.ratio !== undefined) as {
    skin: string;
    fg: string;
    bg: string;
    ratio: number;
  }[];
  // Only a clear group box and a clear backdrop leave a pair without a color.
  expect(rows.length).toBeGreaterThan(SKINS.length * (TEXT_PAIRS.length - 12));
  expect(
    rows
      .filter((r) => !(r.ratio >= 7))
      .map((r) => `${r.skin}: ${r.fg} on ${r.bg} = ${r.ratio.toFixed(2)}`),
  ).toEqual([]);
  const worst = SKINS.map(({ id }) => {
    const r = rows.filter((x) => x.skin === id).sort((a, b) => a.ratio - b.ratio)[0];
    return `${id}: ${r.ratio.toFixed(2)} (${r.fg} on ${r.bg})`;
  });
  console.log(`lowest text contrast per skin\n  ${worst.join("\n  ")}`);
});

test("control edges, state icons and the focus ring meet 3:1 in every skin", () => {
  const failures: string[] = [];
  for (const { id } of SKINS) {
    const t = tokens(id);
    for (const [name, fgs, bg] of UI_PAIRS) {
      const r = ratio(t, fgs, [bg], true);
      if (bg !== "group" && r === undefined) failures.push(`${id}: ${name} on ${bg} has no color`);
      if (r !== undefined && !(r >= 3)) failures.push(`${id}: ${name} on ${bg} = ${r.toFixed(2)}`);
    }
    // The scroll thumb against its track, or against what shows through a clear track.
    const track = colorsOf(t, "scroll-track").length ? ["scroll-track"] : ["surface", "panel"];
    const thumb = ratio(t, ["scroll-thumb", "scroll-thumb-bevel"], track, true);
    if (!(thumb !== undefined && thumb >= 3)) failures.push(`${id}: scroll thumb = ${thumb}`);
  }
  expect(failures).toEqual([]);
});

test("the focus ring is solid and at least 2 px wide in every skin", () => {
  for (const { id } of SKINS) {
    expect(tokens(id).get("--focus-style"), id).toBe("solid");
    expect(parseFloat(tokens(id).get("--focus-width") ?? ""), id).toBeGreaterThanOrEqual(2);
  }
});
