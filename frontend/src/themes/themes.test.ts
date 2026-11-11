// The skin contract (openspec ui-themes): every skin defines the same tokens, colors live only in this
// folder, corners are square, and text meets 4.5:1 in every skin.
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

test("the six shipped skins are registered, each with its own stylesheet", () => {
  expect(SKINS.map((s) => s.id)).toEqual(["light", "dark", "xp", "macos", "ubuntu", "win98"]);
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

test("corners are square in every skin and no source rounds them", () => {
  for (const { id } of SKINS) expect(tokens(id).get("--radius"), id).toBe("0");
  for (const [file, text] of Object.entries(sources)) {
    expect(text, file).not.toMatch(/\brounded(-|\b)/);
    expect(text.replaceAll("border-radius: var(--radius)", ""), file).not.toContain(
      "border-radius",
    );
  }
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

const STATES = ["active", "ringing", "held", "muted", "error", "secure", "insecure"];
/** Text token on background token, as the components pair them (style.css). */
const TEXT_PAIRS: [string, string][] = [
  ["text", "surface"],
  ["text", "panel"],
  ["text", "input"],
  ["text", "hover"],
  ["text-muted", "surface"],
  ["text-muted", "panel"],
  ["text-muted", "input"],
  ["text-muted", "hover"],
  ["link", "surface"],
  ["link", "panel"],
  ["accent-text", "accent"],
  ["selection-text", "selection"],
  ["btn-text", "btn"],
  ["btn-text", "btn-hover"],
  ["btn-text", "btn-active"],
  ["key-text", "key"],
  ["key-sub", "key"],
  ["call-text", "call"],
  ["hangup-text", "hangup"],
  ["notice-text", "notice"],
  ["title-text", "title"],
  ["tab-text", "tab"],
  ["tab-text", "tab-bar"],
  ["tab-active-text", "tab-active"],
  ["status-text", "status"],
  ...STATES.flatMap((s): [string, string][] => [
    [`state-${s}`, "surface"],
    [`state-${s}`, "panel"],
  ]),
];

/** The lowest contrast of each pair in each skin, for the report in docs/ui/go-frontend.md. */
function audit(): { skin: string; fg: string; bg: string; ratio: number }[] {
  return SKINS.flatMap(({ id }) => {
    const t = tokens(id);
    return TEXT_PAIRS.map(([fg, bg]) => {
      const ratios = colors(t.get(`--${fg}`) ?? "").flatMap((f) =>
        colors(t.get(`--${bg}`) ?? "").map((b) => contrast(f, b)),
      );
      return { skin: id, fg, bg, ratio: Math.min(Infinity, ...ratios) };
    });
  });
}

test("text meets 4.5:1 on its background in every skin", () => {
  expect(contrast("#000000", "#ffffff")).toBeCloseTo(21);
  const rows = audit();
  expect(rows.length).toBe(SKINS.length * TEXT_PAIRS.length);
  expect(
    rows
      .filter((r) => !(r.ratio >= 4.5))
      .map((r) => `${r.skin}: ${r.fg} on ${r.bg} = ${r.ratio.toFixed(2)}`),
  ).toEqual([]);
  const worst = SKINS.map(({ id }) => {
    const r = rows.filter((x) => x.skin === id).sort((a, b) => a.ratio - b.ratio)[0];
    return `${id}: ${r.ratio.toFixed(2)} (${r.fg} on ${r.bg})`;
  });
  console.log(`lowest text contrast per skin\n  ${worst.join("\n  ")}`);
});

test("the focus ring meets 3:1 in every skin", () => {
  const failures: string[] = [];
  for (const { id } of SKINS) {
    const t = tokens(id);
    for (const [fg, bg] of [
      ["focus", "surface"],
      ["focus", "panel"],
    ]) {
      const ratio = contrast(
        colors(t.get(`--${fg}`) ?? "")[0] ?? "",
        colors(t.get(`--${bg}`) ?? "")[0] ?? "",
      );
      if (!(ratio >= 3)) failures.push(`${id}: ${fg} on ${bg} = ${ratio.toFixed(2)}`);
    }
  }
  expect(failures).toEqual([]);
});
