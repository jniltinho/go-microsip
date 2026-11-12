<script setup lang="ts">
// Original line icons drawn for this application (24x24, the stroke takes the text color), so the
// bundle carries no third-party icon set. Decorative by default: the label next to it is the name.
const PATHS = {
  phone:
    "M5 3h4l2 5-2.5 1.5a11 11 0 0 0 6 6L16 13l5 2v4a2 2 0 0 1-2 2A16 16 0 0 1 3 5a2 2 0 0 1 2-2z",
  hangup: "M2 14a14 14 0 0 1 20 0v3l-5 1-1-4a12 12 0 0 0-8 0l-1 4-5-1z",
  in: "M19 5 6 18M6 9v9h9",
  out: "M5 19 18 6M9 6h9v9",
  missed: "M4 6l8 8 8-8M4 12V6h6M4 20h16",
  mic: "M9 3h6v9H9zM5 11a7 7 0 0 0 14 0M12 18v3",
  micOff: "M9 3h6v9H9zM5 11a7 7 0 0 0 14 0M12 18v3M3 3l18 18",
  pause: "M8 5v14M16 5v14",
  play: "M7 4l13 8-13 8z",
  lock: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4",
  unlock: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 7.5-2",
  transfer: "M4 8h14M14 4l4 4-4 4M20 16H6M10 12l-4 4 4 4",
  star: "M12 3l2.7 5.8 6.3.8-4.6 4.3 1.2 6.3L12 17l-5.6 3.2 1.2-6.3L3 9.6l6.3-.8z",
  search: "M10.5 4a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM15.5 15.5 20 20",
  settings: "M4 7h9M17 7h3M4 17h3M11 17h9M13 4v6h4V4zM7 14v6h4v-6z",
  down: "M6 9l6 6 6-6",
  close: "M6 6l12 12M18 6 6 18",
  backspace: "M9 5h12v14H9l-6-7zM12 9l5 6M17 9l-5 6",
  check: "M5 12l5 5 9-10",
  alert: "M12 4 2.5 20h19zM12 10v5M12 17.5v.5",
  ok: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM8 12l3 3 5-6",
  wait: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 7v5l3 2",
  off: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM5.6 5.6l12.8 12.8",
  dnd: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM7.5 12h9",
  user: "M12 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM4 21a8 8 0 0 1 16 0",
  plus: "M12 5v14M5 12h14",
  trash: "M4 7h16M9 7V4h6v3M6 7l1 14h10l1-14",
  edit: "M4 20l1-5L16 4l4 4L9 19zM14 6l4 4",
  bell: "M6 17v-6a6 6 0 0 1 12 0v6l2 2H4zM10 22h4",
  home: "M4 11l8-7 8 7v9h-5v-6H9v6H4z",
  keypad: "M6 5h.01M12 5h.01M18 5h.01M6 12h.01M12 12h.01M18 12h.01M6 19h.01M12 19h.01M18 19h.01",
} as const;
export type IconName = keyof typeof PATHS;

defineProps<{ name: IconName; filled?: boolean }>();
</script>

<template>
  <svg
    class="inline-block size-[1.15em] shrink-0"
    viewBox="0 0 24 24"
    :fill="filled ? 'currentColor' : 'none'"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="square"
    stroke-linejoin="miter"
    aria-hidden="true"
  >
    <path :d="PATHS[name]" />
  </svg>
</template>
