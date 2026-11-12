// Focus-ring audit of the page as drawn, evaluated in the browser by prints.sh after contrast.js
// (whose arithmetic it uses) and after a real key press, so the browser shows focus as it does for
// a person at the keyboard. Every control in sight that takes focus is focused in turn and must
// show a ring: an outline that is drawn, at least 2 px wide, and at 3:1 against what it is drawn
// on (the control itself for a ring inside it, what the control stands on for a ring around it).
// Returns "<controls checked> <what is wrong>"; the focus goes back where it was.
(() => {
  const { parse, ratio, backgrounds } = window.__aaa;
  const modal = document.querySelector("dialog[open]");
  const before = document.activeElement;
  const failures = [];
  let checked = 0;
  for (const el of document.querySelectorAll(
    'button, input, select, textarea, a[href], [tabindex]:not([tabindex="-1"])',
  )) {
    if ((modal && !modal.contains(el)) || el.closest(":disabled")) continue;
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
    const low = Math.min(...on.map((bg) => ratio(parse(style.outlineColor), bg)));
    if (low < 3) failures.push(`${low.toFixed(2)} ring on "${name}"`);
  }
  if (before instanceof HTMLElement) before.focus();
  else if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  return `${checked} ${failures.join("; ")}`;
})();
