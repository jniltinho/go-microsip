// Contrast audit of the page as drawn, evaluated in the browser by prints.sh on every screen: each
// visible text, field value and placeholder against the nearest painted background (every stop of
// a gradient). Returns the texts below 4.5:1. Inactive controls are exempt (WCAG 1.4.3), and so
// is what lies behind an open modal dialog, which is inert.
(() => {
  const parse = (c) => {
    const m = /rgba?\(([^)]+)\)/.exec(c);
    if (!m) return null;
    const [r, g, b, a = 1] = m[1].split(/[\s,/]+/).map(Number);
    return { r, g, b, a };
  };
  const channel = (v) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  const luminance = ({ r, g, b }) =>
    0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
  const ratio = (a, b) => {
    const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
    return (hi + 0.05) / (lo + 0.05);
  };
  const backgrounds = (el) => {
    for (let e = el; e; e = e.parentElement) {
      const style = getComputedStyle(e);
      if (style.backgroundImage !== "none")
        return (style.backgroundImage.match(/rgba?\([^)]+\)/g) ?? []).map(parse);
      const color = parse(style.backgroundColor);
      if (color && color.a > 0) return [color];
    }
    return [{ r: 255, g: 255, b: 255, a: 1 }];
  };
  const modal = document.querySelector("dialog[open]");
  const failures = new Set();
  const audit = (el, color, text) => {
    if (!text || (modal && !modal.contains(el)) || el.closest(":disabled")) return;
    const box = el.getBoundingClientRect();
    if (!box.width || !box.height) return;
    const fg = parse(color);
    const worst = Math.min(...backgrounds(el).map((bg) => ratio(fg, bg)));
    if (worst < 4.5) failures.add(`${worst.toFixed(2)} "${text.slice(0, 30)}"`);
  };
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let node; (node = walker.nextNode());) {
    const el = node.parentElement;
    if (el && !el.closest("select, script, style"))
      audit(el, getComputedStyle(el).color, node.textContent.trim());
  }
  for (const el of document.querySelectorAll(
    "input:not([type=checkbox]):not([type=radio]), select",
  )) {
    audit(el, getComputedStyle(el).color, el.value || "(value)");
    if (el.placeholder) audit(el, getComputedStyle(el, "::placeholder").color, el.placeholder);
  }
  return [...failures].join("; ");
})();
