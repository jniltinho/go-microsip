<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { type Scenario, scenarios, simulate } from "../api";
import { moveFocus } from "../keys";
import { ui } from "../store";
import Icon from "./Icon.vue";

const open = ref(false);
const root = ref<HTMLElement>();
const trigger = ref<HTMLButtonElement>();
const menu = ref<HTMLElement>();
const scenes = ref<Scenario[]>([]);

async function toggle() {
  open.value = !open.value;
  await nextTick();
  menu.value?.querySelector<HTMLElement>("button")?.focus();
}
function close() {
  open.value = false;
  trigger.value?.focus();
}
function pick(run: () => unknown) {
  close();
  void run();
}
function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.stopPropagation();
    close();
  } else if (e.key === "Tab") open.value = false;
  else moveFocus(e, "button");
}
const outside = (e: Event) => {
  if (!root.value?.contains(e.target as Node)) open.value = false;
};
onMounted(async () => {
  window.addEventListener("pointerdown", outside);
  scenes.value = await scenarios().catch(() => []);
});
onBeforeUnmount(() => window.removeEventListener("pointerdown", outside));
</script>

<template>
  <div ref="root" class="relative ml-auto">
    <button ref="trigger" class="btn" aria-haspopup="menu" :aria-expanded="open" @click="toggle">
      Menu<Icon name="down" />
    </button>
    <div
      v-if="open"
      ref="menu"
      class="menu absolute top-full right-0 z-10"
      role="menu"
      aria-label="Menu"
      @keydown="onKey"
    >
      <button
        class="menuitem"
        role="menuitem"
        @click="pick(() => ui.dispatch({ type: 'openSettings' }))"
      >
        <Icon name="settings" />Settings
      </button>
      <button
        class="menuitem"
        role="menuitemcheckbox"
        :aria-checked="ui.dnd"
        @click="pick(() => ui.dispatch({ type: 'toggleDnd' }))"
      >
        <Icon :name="ui.dnd ? 'check' : 'dnd'" />Do not disturb{{ ui.dnd ? ": on" : "" }}
      </button>
      <button
        v-if="ui.registration.state === 'registered' || ui.registration.state === 'registering'"
        class="menuitem"
        role="menuitem"
        @click="pick(() => ui.dispatch({ type: 'unregister' }))"
      >
        <Icon name="off" />Unregister
      </button>
      <button
        v-else
        class="menuitem"
        role="menuitem"
        @click="pick(() => ui.dispatch({ type: 'register' }))"
      >
        <Icon name="ok" />Register
      </button>
      <template v-if="scenes.length">
        <div class="my-0.5 border-t border-line-subtle" role="separator"></div>
        <div class="px-2 py-0.5 text-[0.9rem] text-muted" role="presentation">
          Simulate (mock engine)
        </div>
        <button
          v-for="s in scenes"
          :key="s.id"
          class="menuitem"
          role="menuitem"
          @click="pick(() => simulate(s.id))"
        >
          <Icon name="play" />{{ s.label }}
        </button>
      </template>
      <div class="my-0.5 border-t border-line-subtle" role="separator"></div>
      <button class="menuitem" role="menuitem" @click="pick(() => ui.dispatch({ type: 'quit' }))">
        <Icon name="close" />Quit
      </button>
    </div>
  </div>
</template>
