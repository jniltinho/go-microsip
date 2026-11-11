<script setup lang="ts">
import { MAX_NUMBER_CHARS } from "../state";
import { ui } from "../store";
import CallPanel from "./CallPanel.vue";
import Icon from "./Icon.vue";

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
const WHEN_CONNECTED = "Available once a call is connected";
</script>

<template>
  <section class="flex h-full flex-col gap-1.5 p-2" aria-label="Dialpad">
    <CallPanel v-if="ui.call" />
    <input
      v-else
      id="number"
      v-model="ui.number"
      class="field num text-[1.35rem]"
      :maxlength="MAX_NUMBER_CHARS"
      inputmode="tel"
      placeholder="Number"
      aria-label="Number to call"
      autocomplete="off"
      spellcheck="false"
    />
    <div
      class="grid flex-1 auto-rows-fr grid-cols-3 gap-1"
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
        ><small>{{ letters }}</small>
      </button>
    </div>
    <div class="grid grid-cols-3 gap-1">
      <button
        class="btn"
        :disabled="!!ui.call"
        aria-label="Backspace"
        @click="ui.dispatch({ type: 'backspace' })"
      >
        <Icon name="backspace" />
      </button>
      <button
        class="btn"
        :disabled="!!ui.call"
        aria-label="Plus"
        @click="ui.dispatch({ type: 'plus' })"
      >
        +
      </button>
      <button
        class="btn"
        :disabled="!!ui.call"
        aria-label="Clear number"
        @click="ui.dispatch({ type: 'clearNumber' })"
      >
        C
      </button>
    </div>
    <div class="grid grid-cols-3 gap-1">
      <button
        class="btn btn-call col-span-2 min-h-[2.6rem]"
        :disabled="ui.cannotCall !== null"
        :title="ui.cannotCall ?? undefined"
        @click="ui.dispatch({ type: 'call' })"
      >
        <Icon name="phone" />{{ ui.callLabel }}
      </button>
      <button
        class="btn btn-hangup min-h-[2.6rem]"
        :disabled="ui.cannotEnd !== null"
        :title="ui.cannotEnd ?? undefined"
        @click="ui.dispatch({ type: 'end' })"
      >
        <Icon name="hangup" />{{ ui.endLabel }}
      </button>
    </div>
    <div class="grid grid-cols-4 gap-1" role="group" aria-label="Call controls">
      <button
        class="btn px-1"
        :aria-pressed="ui.dnd"
        title="Do not disturb"
        @click="ui.dispatch({ type: 'toggleDnd' })"
      >
        <Icon :name="ui.dnd ? 'check' : 'dnd'" />DND
      </button>
      <button
        class="btn px-1"
        :disabled="!ui.connectedCall"
        :aria-pressed="!!ui.connectedCall?.muted"
        :title="ui.connectedCall ? undefined : WHEN_CONNECTED"
        @click="ui.dispatch({ type: 'toggleMute' })"
      >
        <Icon :name="ui.connectedCall?.muted ? 'micOff' : 'mic'" />{{
          ui.connectedCall?.muted ? "Unmute" : "Mute"
        }}
      </button>
      <button
        class="btn px-1"
        :disabled="!ui.connectedCall"
        :aria-pressed="ui.connectedCall?.phase === 'held'"
        :title="ui.connectedCall ? undefined : WHEN_CONNECTED"
        @click="ui.dispatch({ type: 'toggleHold' })"
      >
        <Icon :name="ui.connectedCall?.phase === 'held' ? 'play' : 'pause'" />{{
          ui.connectedCall?.phase === "held" ? "Resume" : "Hold"
        }}
      </button>
      <button
        class="btn px-1"
        :disabled="!ui.connectedCall"
        :aria-pressed="ui.transferTarget !== null"
        :title="ui.connectedCall ? 'Blind transfer' : WHEN_CONNECTED"
        @click="
          ui.dispatch({ type: ui.transferTarget === null ? 'openTransfer' : 'cancelTransfer' })
        "
      >
        <Icon name="transfer" />Xfer
      </button>
    </div>
  </section>
</template>
