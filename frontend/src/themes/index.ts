// Skin registry (Settings > Appearance). Each skin is a CSS file in this folder keyed by data-theme on
// <html>; every skin defines the same tokens (themes.test.ts). "system" resolves to light or dark
// and follows the desktop live. Adding a skin: one CSS file, one @import in index.css, one entry here.
export type Skin = { id: string; label: string; dark: boolean };

export const SKINS: readonly Skin[] = [
  { id: "light", label: "Light", dark: false },
  { id: "dark", label: "Dark", dark: true },
  { id: "xp", label: "Windows XP", dark: false },
  { id: "macos", label: "macOS", dark: false },
  { id: "ubuntu", label: "Ubuntu", dark: false },
  { id: "win98", label: "Windows 98", dark: false },
];
export const SYSTEM = "system";
/** What settings offers: System first, then every skin. */
export const SKIN_CHOICES: readonly { id: string; label: string }[] = [
  { id: SYSTEM, label: "System" },
  ...SKINS,
];

const CACHE = "go-microsip.skin";
const os =
  typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : undefined;
let chosen = SYSTEM;

/** The setting's value for a stored id: unknown or removed skins resolve to "system". */
export const knownSkin = (id: string | null | undefined) =>
  SKINS.some((s) => s.id === id) ? (id as string) : SYSTEM;

/** The skin actually drawn for a setting: "system" (and anything unknown) follows the desktop. */
export function effectiveSkin(id: string, prefersDark: boolean): Skin {
  return SKINS.find((s) => s.id === id) ?? SKINS[prefersDark ? 1 : 0];
}

function paint() {
  const el = document.documentElement;
  el.dataset.theme = effectiveSkin(chosen, os?.matches ?? false).id;
  el.dataset.skin = chosen;
}

/** Apply the skin setting to the whole window at once and remember it for the next first paint. */
export function applySkin(id: string) {
  chosen = knownSkin(id);
  paint();
  try {
    localStorage.setItem(CACHE, chosen);
  } catch {
    // Storage is blocked: the backend's saved setting still applies after start.
  }
}

/** The skin applied last time, so the window does not flash another skin while settings load. */
export function cachedSkin(): string {
  try {
    return knownSkin(localStorage.getItem(CACHE));
  } catch {
    return SYSTEM;
  }
}

os?.addEventListener("change", () => {
  if (chosen === SYSTEM) paint();
});
