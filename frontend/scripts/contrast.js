// Contrast audit of the page as drawn (WCAG AAA), evaluated in the browser by prints.sh on every
// screen: each visible text, field value and placeholder against the nearest painted background
// (every stop of a gradient) at 7:1; each icon, and the edge of each button, key, call control,
// field, check box and radio (its border, an opaque line of its bevel or its face) against what it
// stands on, at 3:1, each as it ends up drawn: at the opacity of the element and of everything it
// is inside of. Returns what
// is below. Inactive controls are exempt (WCAG 1.4.6, 1.4.11), and so is what lies behind an open
// modal dialog, which is inert.
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
      const color = parse(style.backgroundColor);
      // Layers: every opaque stop, and the color under them when there is one.
      if (style.backgroundImage !== "none") {
        const stops = (style.backgroundImage.match(/rgba?\([^)]+\)/g) ?? []).map(parse);
        return [...stops, color].filter((c) => c && c.a > 0);
      }
      if (color && color.a > 0) return [color];
    }
    return [{ r: 255, g: 255, b: 255, a: 1 }];
  };
  // What is drawn is what the element's color becomes over its background at the opacity the
  // element ends up with: its own and that of everything it is inside of.
  const alpha = (el) => {
    let a = 1;
    for (let e = el; e; e = e.parentElement) a *= Number(getComputedStyle(e).opacity);
    return a;
  };
  // A color that is itself partly or wholly transparent shows that much less.
  const over = (fg, bg, opacity) => {
    const a = opacity * (fg.a ?? 1);
    return {
      r: fg.r * a + bg.r * (1 - a),
      g: fg.g * a + bg.g * (1 - a),
      b: fg.b * a + bg.b * (1 - a),
      a: 1,
    };
  };
  // What a shape of an icon paints: its stroke or its fill, whichever shows more, with the
  // opacity it ends up with; null when it paints nothing (hidden, no paint, a paint or a line
  // of no opacity or width).
  const paint = (shape) => {
    const s = getComputedStyle(shape);
    if (s.display === "none" || s.visibility === "hidden") return null;
    const stroke = parse(s.stroke);
    const fill = parse(s.fill);
    const strokeA =
      stroke && parseFloat(s.strokeWidth) > 0 ? stroke.a * Number(s.strokeOpacity) : 0;
    const fillA = fill ? fill.a * Number(s.fillOpacity) : 0;
    const [color, a] = strokeA >= fillA ? [stroke, strokeA] : [fill, fillA];
    return a > 0 && Number(s.opacity) > 0 ? { color, a: a * Number(s.opacity) } : null;
  };
  // An icon is its shapes: the one that shows most is what is seen of it. null: nothing is.
  const icon = (svg) => {
    let best = null;
    for (const shape of svg.querySelectorAll(
      "path, circle, rect, line, polyline, polygon, ellipse",
    )) {
      const p = paint(shape);
      if (p && (!best || p.a > best.a)) best = p;
    }
    const a = best ? best.a * alpha(svg) : 0;
    return a > 0 ? { color: best.color, a } : null;
  };
  // For focus.js and the state check, which use the same arithmetic.
  window.__aaa = { parse, ratio, backgrounds, alpha, over, icon };
  const modal = document.querySelector("dialog[open]");
  const failures = new Set();
  const shown = (el) => {
    if ((modal && !modal.contains(el)) || el.closest(":disabled")) return false;
    const box = el.getBoundingClientRect();
    return box.width > 0 && box.height > 0;
  };
  const worst = (fg, bgs, a = 1) => Math.min(...bgs.map((bg) => ratio(over(fg, bg, a), bg)));
  const audit = (el, color, text) => {
    if (!text || !shown(el)) return;
    const low = worst(parse(color), backgrounds(el), alpha(el));
    if (low < 7) failures.add(`${low.toFixed(2)} "${text.slice(0, 30)}"`);
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
  for (const el of document.querySelectorAll("svg")) {
    if (!shown(el)) continue;
    const drawn = icon(el);
    const low = drawn ? worst({ ...drawn.color, a: 1 }, backgrounds(el), drawn.a) : 1;
    if (low < 3)
      failures.add(`${low.toFixed(2)} icon in "${el.parentElement.innerText.trim().slice(0, 20)}"`);
  }
  for (const el of document.querySelectorAll(
    ".btn, .key, .ctl-face, .field, input[type=checkbox], input[type=radio]",
  )) {
    if (!shown(el)) continue;
    const style = getComputedStyle(el);
    const around = backgrounds(el.parentElement);
    const lines = (style.boxShadow.match(/rgba?\([^)]+\)/g) ?? []).map(parse);
    if (parseFloat(style.borderTopWidth) > 0) lines.push(parse(style.borderTopColor));
    const a = alpha(el);
    const edge = Math.max(
      Math.min(...backgrounds(el).map((face) => worst(face, around, a))),
      ...lines.filter((c) => c && c.a === 1).map((c) => worst(c, around, a)),
    );
    if (edge < 3)
      failures.add(
        `${edge.toFixed(2)} edge of "${(el.innerText || el.ariaLabel || el.tagName).trim().slice(0, 20)}"`,
      );
  }
  return [...failures].join("; ");
})();
