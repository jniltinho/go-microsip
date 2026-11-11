<script setup lang="ts">
// The current call, drawn only from the engine's latest snapshot. Every state is an icon and a label.
import { computed } from "vue";
import { formatDuration, phaseText, transferText } from "../state";
import { nameOf, now, ui } from "../store";
import Icon, { type IconName } from "./Icon.vue";

const call = computed(() => ui.call?.snapshot);
const name = computed(() => (call.value ? nameOf(call.value.remote) : undefined));
const phase = computed((): { icon: IconName; tone: string } => {
  switch (call.value?.phase) {
    case "established":
      return { icon: "phone", tone: "text-active" };
    case "held":
      return { icon: "pause", tone: "text-held" };
    case "ringing":
      return { icon: "bell", tone: "text-ringing" };
    case "dialing":
      return { icon: "out", tone: "text-ringing" };
    default:
      return { icon: "hangup", tone: "text-muted" };
  }
});
const elapsed = computed(() => {
  const ms = ui.duration(now.value);
  return ms === null ? "" : formatDuration(ms);
});
</script>

<template>
  <div v-if="call" class="well flex flex-col gap-1.5 p-2" role="group" aria-label="Current call">
    <div class="flex items-baseline gap-2">
      <span class="text-[0.9rem] text-muted">{{
        call.direction === "incoming" ? "From" : "To"
      }}</span>
      <!-- Remote-controlled text: interpolation only. -->
      <span class="min-w-0 flex-1 truncate text-[1.25rem] font-semibold">{{
        name ?? call.remote
      }}</span>
      <span v-if="elapsed" class="num text-[1.1rem]" aria-label="Duration">{{ elapsed }}</span>
    </div>
    <div v-if="name" class="num truncate text-muted">{{ call.remote }}</div>
    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
      <span class="flex items-center gap-1 font-semibold" :class="phase.tone" role="status">
        <Icon :name="phase.icon" />{{ phaseText(call) }}
      </span>
      <span v-if="call.muted" class="badge text-mute"><Icon name="micOff" />Muted</span>
      <span v-if="call.secure" class="badge text-secure"><Icon name="lock" />Encrypted (SRTP)</span>
      <span v-else class="badge text-insecure"><Icon name="unlock" />Unencrypted (RTP)</span>
    </div>
    <div
      v-if="call.transfer"
      class="flex items-center gap-1"
      :class="call.transfer.state === 'failed' ? 'text-error' : 'text-ringing'"
      role="status"
    >
      <Icon :name="call.transfer.state === 'failed' ? 'alert' : 'transfer'" />{{
        transferText(call.transfer)
      }}
    </div>
    <form
      v-if="ui.transferTarget !== null"
      class="flex gap-1"
      aria-label="Blind transfer"
      @submit.prevent="ui.dispatch({ type: 'confirmTransfer' })"
      @keydown.esc.stop="ui.dispatch({ type: 'cancelTransfer' })"
    >
      <input
        v-model="ui.transferTarget"
        class="field num"
        maxlength="64"
        placeholder="Transfer to"
        aria-label="Transfer to"
        autocomplete="off"
        spellcheck="false"
        autofocus
      />
      <button class="btn" type="submit">Transfer</button>
      <button class="btn" type="button" @click="ui.dispatch({ type: 'cancelTransfer' })">
        Cancel
      </button>
    </form>
  </div>
</template>
