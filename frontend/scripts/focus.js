// Focus-ring audit of the page as drawn, evaluated in the browser by prints.sh after contrast.js
// (whose arithmetic it uses). Every control in sight that takes focus is focused in turn, as the
// keyboard would, and must show a ring: an outline that is drawn, at least 2 px wide, and at 3:1
// against what it is drawn on (the control itself for a ring inside it, what the control stands
// on for a ring around it), at the opacity the control ends up with. With a dialog or the menu
// open, the controls audited are those of that layer: the rest is not reachable. Returns
// "<controls checked> <what is wrong>"; the focus goes back where it was.
(() => {
  const { parse, ratio, backgrounds, alpha, over } = window.__aaa;
  const layer = document.querySelector("dialog[open]") ?? document.querySelector('[role="menu"]');
  const before = document.activeElement;
  const failures = [];
  let checked = 0;
  for (const el of document.querySelectorAll(
    'button, input, select, textarea, a[href], [tabindex]:not([tabindex="-1"])',
  )) {
    if ((layer && !layer.contains(el)) || el.closest(":disabled")) continue;
    if (el.getAttribute("tabindex") === "-1") continue;
    const box = el.getBoundingClientRect();
    if (box.width === 0 || box.height === 0) continue;
    el.focus();
    if (document.activeElement !== el) continue;
    const name = (el.innerText || el.getAttribute("aria-label") || el.placeholder || el.tagName)
      .trim()
      .slice(0, 20);
    checked++;
    const style = getComputedStyle(el);
    const width = parseFloat(style.outlineWidth);
    if (!el.matches(":focus-visible") || style.outlineStyle === "none" || !(width >= 2)) {
      failures.push(`no ring on "${name}" (${style.outlineStyle} ${width}px)`);
      continue;
    }
    const on =
      parseFloat(style.outlineOffset) < 0 ? backgrounds(el) : backgrounds(el.parentElement);
    const a = alpha(el);
    const low = Math.min(...on.map((bg) => ratio(over(parse(style.outlineColor), bg, a), bg)));
    if (low < 3) failures.push(`${low.toFixed(2)} ring on "${name}"`);
  }
  if (before instanceof HTMLElement && document.contains(before)) before.focus();
  else if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  return `${checked} ${failures.join("; ")}`;
})();
