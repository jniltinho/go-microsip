<script setup lang="ts">
// The Keypad destination: the number and the keys, or the current call when there is one.
import { MAX_NUMBER_CHARS } from "../state";
import { nameOf, ui } from "../store";
import CallPanel from "./CallPanel.vue";
import Icon from "./Icon.vue";
import KeyGrid from "./KeyGrid.vue";
</script>

<template>
  <CallPanel v-if="ui.call" />
  <section v-else class="flex h-full flex-col gap-2 px-3 pb-3" aria-label="Keypad">
    <div class="flex items-start gap-2">
      <div class="min-w-0 flex-1 text-center">
        <input
          id="number"
          v-model="ui.number"
          class="field pill num min-h-[3.4rem] text-center text-[1.9rem] font-semibold placeholder:font-normal"
          :maxlength="MAX_NUMBER_CHARS"
          inputmode="tel"
          placeholder="Number"
          aria-label="Number to call"
          autocomplete="off"
          spellcheck="false"
        />
        <!-- The contact this number belongs to: interpolation only. -->
        <p class="mt-1 h-[1.4em] truncate text-[0.9rem] text-muted" aria-live="polite">
          {{ nameOf(ui.number) }}
        </p>
      </div>
      <button
        v-if="ui.number"
        class="btn pill min-h-[3.4rem] w-[3.4rem] shrink-0 p-0"
        aria-label="Clear number"
        @click="ui.dispatch({ type: 'clearNumber' })"
      >
        <Icon name="close" />
      </button>
    </div>
    <KeyGrid>
      <button class="key" aria-label="Plus" @click="ui.dispatch({ type: 'plus' })">+</button>
      <button class="btn btn-call pill big" @click="ui.dispatch({ type: 'call' })">
        <Icon name="phone" />{{ ui.callLabel }}
      </button>
      <button class="key" aria-label="Backspace" @click="ui.dispatch({ type: 'backspace' })">
        <Icon name="backspace" />
      </button>
    </KeyGrid>
  </section>
</template>
