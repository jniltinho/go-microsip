<script setup lang="ts">
// The current call, drawn only from the engine's latest snapshot. Every state is an icon and a
// label. The controls appear once the call is connected; a control that cannot work is not drawn.
import { computed, ref } from "vue";
import { formatDuration, phaseText, transferText } from "../state";
import { initials, nameOf, now, ui } from "../store";
import Icon, { type IconName } from "./Icon.vue";
import KeyGrid from "./KeyGrid.vue";

const call = computed(() => ui.call?.snapshot);
const name = computed(() => (call.value ? nameOf(call.value.remote) : undefined));
const phase = computed((): { icon: IconName; tone: string } => {
  switch (call.value?.phase) {
    case "established":
      return { icon: "phone", tone: "bg-active-soft text-active" };
    case "held":
      return { icon: "pause", tone: "bg-held-soft text-held" };
    case "ringing":
      return { icon: "bell", tone: "bg-ringing-soft text-ringing" };
    case "dialing":
      return { icon: "out", tone: "bg-ringing-soft text-ringing" };
    default:
      return { icon: "hangup", tone: "text-muted" };
  }
});
const elapsed = computed(() => {
  const ms = ui.duration(now.value);
  return ms === null ? "" : formatDuration(ms);
});
/** The keys take the caller's place to send DTMF. */
const pad = ref(false);
</script>

<template>
  <section
    v-if="call"
    class="flex h-full flex-col items-center gap-2 px-4 pb-3"
    role="group"
    aria-label="Current call"
  >
    <div class="flex flex-wrap items-center justify-center gap-1.5">
      <span class="badge" :class="phase.tone" role="status">
        <Icon :name="phase.icon" />{{ phaseText(call) }}
      </span>
      <span v-if="call.muted" class="badge bg-mute-soft text-mute"
        ><Icon name="micOff" />Muted</span
      >
      <span v-if="call.secure" class="badge bg-secure-soft text-secure"
        ><Icon name="lock" />Encrypted (SRTP)</span
      >
      <span v-else class="badge bg-insecure-soft text-insecure"
        ><Icon name="unlock" />Unencrypted (RTP)</span
      >
    </div>
    <div
      v-if="call.transfer"
      class="flex items-center gap-1 font-semibold"
      :class="call.transfer.state === 'failed' ? 'text-error' : 'text-ringing'"
      role="status"
    >
      <Icon :name="call.transfer.state === 'failed' ? 'alert' : 'transfer'" />{{
        transferText(call.transfer)
      }}
    </div>

    <template v-if="pad && ui.connectedCall">
      <!-- Remote-controlled text: interpolation only. -->
      <p class="w-full truncate text-center font-semibold">
        {{ name ?? call.remote }}
        <span v-if="elapsed" class="num font-normal" aria-label="Duration">· {{ elapsed }}</span>
      </p>
      <KeyGrid compact />
    </template>
    <div
      v-else
      class="flex min-h-0 w-full flex-1 flex-col items-center justify-center gap-0.5 overflow-hidden text-center"
    >
      <span v-if="ui.transferTarget === null" class="avatar avatar-big mb-1" aria-hidden="true">{{
        initials(name ?? call.remote)
      }}</span>
      <span class="text-[0.9rem] text-muted">{{
        call.direction === "incoming" ? "From" : "To"
      }}</span>
      <!-- Remote-controlled text: interpolation only. -->
      <span class="w-full truncate text-[1.9rem] leading-tight font-semibold">{{
        name ?? call.remote
      }}</span>
      <span v-if="name" class="num w-full truncate text-[1.15rem] text-muted">{{
        call.remote
      }}</span>
      <span v-if="elapsed" class="num text-[1.6rem]" aria-label="Duration">{{ elapsed }}</span>
    </div>

    <form
      v-if="ui.transferTarget !== null"
      class="flex w-full gap-1"
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
        v-focus
      />
      <button class="btn" type="submit">Transfer</button>
      <button class="btn" type="button" @click="ui.dispatch({ type: 'cancelTransfer' })">
        Cancel
      </button>
    </form>

    <div
      v-if="ui.connectedCall"
      class="grid w-full grid-cols-4 gap-1"
      role="group"
      aria-label="Call controls"
    >
      <button
        class="ctl"
        :aria-pressed="ui.connectedCall.muted"
        @click="ui.dispatch({ type: 'toggleMute' })"
      >
        <span class="ctl-face"><Icon :name="ui.connectedCall.muted ? 'micOff' : 'mic'" /></span
        >{{ ui.connectedCall.muted ? "Unmute" : "Mute" }}
      </button>
      <button
        class="ctl"
        :aria-pressed="ui.connectedCall.phase === 'held'"
        @click="ui.dispatch({ type: 'toggleHold' })"
      >
        <span class="ctl-face"
          ><Icon :name="ui.connectedCall.phase === 'held' ? 'play' : 'pause'" /></span
        >{{ ui.connectedCall.phase === "held" ? "Resume" : "Hold" }}
      </button>
      <button class="ctl" :aria-pressed="pad" title="Keys that send DTMF tones" @click="pad = !pad">
        <span class="ctl-face"><Icon :name="pad ? 'user' : 'keypad'" /></span
        >{{ pad ? "Caller" : "Keypad" }}
      </button>
      <button
        class="ctl"
        :aria-pressed="ui.transferTarget !== null"
        title="Blind transfer"
        @click="
          ui.dispatch({ type: ui.transferTarget === null ? 'openTransfer' : 'cancelTransfer' })
        "
      >
        <span class="ctl-face"><Icon name="transfer" /></span>Transfer
      </button>
    </div>

    <div class="flex w-full gap-3">
      <button
        class="btn btn-hangup pill big flex-1"
        :disabled="ui.cannotEnd !== null"
        :title="ui.cannotEnd ?? undefined"
        @click="ui.dispatch({ type: 'end' })"
      >
        <Icon name="hangup" />{{ ui.incomingRinging ? ui.endLabel : "End call" }}
      </button>
      <button
        v-if="ui.incomingRinging"
        class="btn btn-call pill big flex-1"
        @click="ui.dispatch({ type: 'call' })"
      >
        <Icon name="phone" />{{ ui.callLabel }}
      </button>
    </div>
  </section>
</template>
