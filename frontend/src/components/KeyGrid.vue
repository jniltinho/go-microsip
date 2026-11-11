<script setup lang="ts">
// The twelve keys. They type the number, or send DTMF while a call is established (the reducer
// decides). The slot adds a row under them. `compact` is for the keys inside a call: no letters,
// and keys that may be wider than tall, so they stay usable in the shortest window.
import { ui } from "../store";

defineProps<{ compact?: boolean }>();

const KEYS = [
  ["1", ""],
  ["2", "ABC"],
  ["3", "DEF"],
  ["4", "GHI"],
  ["5", "JKL"],
  ["6", "MNO"],
  ["7", "PQRS"],
  ["8", "TUV"],
  ["9", "WXYZ"],
  ["*", ""],
  ["0", ""],
  ["#", ""],
];
</script>

<template>
  <div
    class="keys"
    :class="{ compact }"
    role="group"
    :aria-label="ui.keypadSendsDtmf ? 'Keypad, sends DTMF tones' : 'Keypad'"
  >
    <button
      v-for="[key, letters] in KEYS"
      :key="key"
      class="key"
      :aria-label="key === '*' ? 'Star' : key === '#' ? 'Hash' : key"
      @click="ui.dispatch({ type: 'key', key })"
    >
      <span>{{ key }}</span
      ><small v-if="letters && !compact">{{ letters }}</small>
    </button>
    <slot />
  </div>
</template>
