/** Arrow keys (and Home/End) move focus among the `selector` elements of the handler's element. */
export function moveFocus(e: KeyboardEvent, selector: string, horizontal = false) {
  const items = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>(selector)];
  const at = items.indexOf(document.activeElement as HTMLElement);
  const [back, forward] = horizontal ? ["ArrowLeft", "ArrowRight"] : ["ArrowUp", "ArrowDown"];
  const to =
    e.key === forward
      ? items[(at + 1) % items.length]
      : e.key === back
        ? items[(at - 1 + items.length) % items.length]
        : e.key === "Home"
          ? items[0]
          : e.key === "End"
            ? items[items.length - 1]
            : undefined;
  if (!to) return;
  e.preventDefault();
  to.focus();
}
