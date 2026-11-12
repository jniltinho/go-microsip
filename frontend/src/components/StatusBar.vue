<script setup lang="ts">
import { computed } from "vue";
import { isMock } from "../api";
import { registrationText } from "../state";
import { ui, unavailable } from "../store";
import Icon, { type IconName } from "./Icon.vue";

// The header: the account and the registration state, on every screen. The state is an icon and a
// label; the color only repeats it. The slot holds do-not-disturb and the menu.
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
  <header class="statusbar">
    <div class="min-w-0 flex-1">
      <p v-if="!unavailable" class="num truncate font-semibold" aria-label="Account">
        {{ ui.accountText }}
      </p>
      <p class="flex items-center gap-1 text-[0.9rem]">
        <span class="flex min-w-0 items-center gap-1" role="status" aria-label="Registration">
          <Icon :name="status.icon" :class="status.tone" />
          <span class="truncate font-semibold">{{ status.label }}</span>
        </span>
        <span v-if="isMock" class="shrink-0" title="Simulated engine with fictional data"
          >· Mock</span
        >
      </p>
    </div>
    <slot />
  </header>
</template>
