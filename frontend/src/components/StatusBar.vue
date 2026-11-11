<script setup lang="ts">
import { computed } from "vue";
import { isMock } from "../api";
import { registrationText } from "../state";
import { ui, unavailable } from "../store";
import Icon, { type IconName } from "./Icon.vue";

// The state is an icon and a label; the color only repeats it.
const status = computed((): { icon: IconName; label: string; tone: string } => {
  if (unavailable.value) return { icon: "alert", label: "Engine unavailable", tone: "text-error" };
  if (ui.engineFailed) return { icon: "alert", label: "Engine stopped", tone: "text-error" };
  if (ui.stopped) return { icon: "off", label: "Stopped", tone: "" };
  const label = registrationText(ui.registration);
  switch (ui.registration.state) {
    case "registered":
      return { icon: "ok", label, tone: "text-active" };
    case "registering":
      return { icon: "wait", label, tone: "text-ringing" };
    case "failed":
      return { icon: "alert", label, tone: "text-error" };
    default:
      return { icon: "off", label, tone: "" };
  }
});
</script>

<template>
  <footer class="statusbar">
    <span class="flex min-w-0 items-center gap-1.5" role="status" aria-label="Registration">
      <Icon :name="status.icon" :class="status.tone" />
      <span class="truncate">{{ status.label }}</span>
    </span>
    <span v-if="ui.dnd" class="flex items-center gap-1"><Icon name="dnd" />DND</span>
    <span v-if="isMock" title="Simulated engine with fictional data">Mock</span>
    <span v-if="!unavailable" class="ml-auto truncate text-[0.9rem]" aria-label="Account">{{
      ui.accountText
    }}</span>
  </footer>
</template>
