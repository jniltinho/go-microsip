<script setup lang="ts">
// The compact softphone window: tabs, the current view, notices, the status bar and the dialogs.
// Everything it shows about the engine comes from store.ui, which changes only on backend events.
import { computed, nextTick, onBeforeUnmount, onMounted } from "vue";
import AppMenu from "./components/AppMenu.vue";
import CallsTab from "./components/CallsTab.vue";
import ContactEditor from "./components/ContactEditor.vue";
import ContactsTab from "./components/ContactsTab.vue";
import Dialog from "./components/Dialog.vue";
import DialpadTab from "./components/DialpadTab.vue";
import Icon from "./components/Icon.vue";
import SettingsView from "./components/SettingsView.vue";
import SkinPicker from "./components/SkinPicker.vue";
import StatusBar from "./components/StatusBar.vue";
import { moveFocus } from "./keys";
import { confirmButton, confirmQuestion, phaseText, TABS } from "./state";
import { about, nameOf, now, start, ui, unavailable } from "./store";

const notice = computed(() => ui.notice(now.value));
const incoming = computed(() => (ui.incomingRinging ? ui.call?.snapshot : undefined));
const approval = computed(() => ui.approvals[0]);
const armed = computed(() => ui.approvalArmed(now.value));

// Keyboard: Ctrl+L number entry, Enter call/answer, Escape hang up/reject or leave a view, and on
// the dialpad digits, * and # type the number or send DTMF. Dialogs and menus handle their own keys.
function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || document.querySelector("dialog[open]")) return;
  const target = e.target as HTMLElement;
  if (e.ctrlKey && e.key.toLowerCase() === "l") {
    e.preventDefault();
    void ui.dispatch({ type: "selectTab", tab: "phone" });
    void nextTick(() => document.getElementById("number")?.focus());
    return;
  }
  if (e.ctrlKey || e.metaKey || e.altKey || unavailable.value || ui.stopped) return;
  if (ui.view !== "main") {
    if (e.key === "Escape") void ui.dispatch({ type: "closeView" });
    return;
  }
  const number = target.id === "number";
  if (target.matches("input, textarea, select") && !number) return;
  if (e.key === "Escape") {
    if (ui.call) void ui.dispatch({ type: "end" });
    return;
  }
  if (ui.tab !== "phone") return;
  if (e.key === "Enter" && (number || !target.closest("button"))) {
    e.preventDefault();
    void ui.dispatch({ type: "call" });
  } else if (number) return;
  else if (/^[0-9*#]$/.test(e.key) || (ui.keypadSendsDtmf && /^[a-d]$/i.test(e.key)))
    void ui.dispatch({ type: "key", key: e.key });
  else if (e.key === "+") void ui.dispatch({ type: "plus" });
  else if (e.key === "Backspace") void ui.dispatch({ type: "backspace" });
}

onMounted(() => {
  window.addEventListener("keydown", onKey);
  void start();
});
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- No engine behind the window (a build without the Go engine, or it stopped): say so. -->
    <main v-if="unavailable || ui.stopped" class="flex flex-1 flex-col gap-3 overflow-auto p-4">
      <h1 class="flex items-center gap-2 text-[1.25rem] font-semibold">
        <Icon :name="ui.stopped ? 'off' : 'alert'" :class="ui.stopped ? '' : 'text-error'" />
        {{ ui.stopped ? "The phone engine stopped" : "Phone engine unavailable" }}
      </h1>
      <p v-if="ui.stopped">Calls and registration ended. You can close the window.</p>
      <template v-else>
        <p>
          This build has no telephony engine behind the window, so no call can be placed or
          received.
        </p>
        <p class="text-muted" role="alert">{{ unavailable }}</p>
      </template>
      <fieldset class="group">
        <legend>Appearance</legend>
        <SkinPicker />
      </fieldset>
      <p class="mt-auto text-[0.9rem] text-muted" data-testid="version">
        {{ about || "go-microsip" }}
      </p>
    </main>

    <template v-else>
      <header class="tabbar">
        <div
          class="tablist"
          role="tablist"
          aria-label="Sections"
          @keydown="moveFocus($event, '.tab', true)"
        >
          <button
            v-for="t in TABS"
            :key="t.id"
            class="tab"
            role="tab"
            :aria-selected="ui.view === 'main' && ui.tab === t.id"
            :tabindex="ui.tab === t.id ? 0 : -1"
            @click="ui.dispatch({ type: 'selectTab', tab: t.id })"
            @focus="ui.view === 'main' && ui.dispatch({ type: 'selectTab', tab: t.id })"
          >
            {{ t.label }}
          </button>
        </div>
        <AppMenu />
      </header>

      <button
        v-if="ui.call && (ui.view !== 'main' || ui.tab !== 'phone')"
        class="notice flex items-center gap-1.5 border-t-0 border-b border-line text-left"
        @click="ui.dispatch({ type: 'selectTab', tab: 'phone' })"
      >
        <Icon name="phone" />
        <span class="min-w-0 flex-1 truncate">
          {{ phaseText(ui.call.snapshot) }}:
          {{ nameOf(ui.call.snapshot.remote) ?? ui.call.snapshot.remote }}
        </span>
        <span class="underline">Show call</span>
      </button>

      <main class="min-h-0 flex-1" role="tabpanel">
        <SettingsView v-if="ui.view === 'settings'" />
        <ContactEditor v-else-if="ui.view === 'contactEdit'" />
        <DialpadTab v-else-if="ui.tab === 'phone'" />
        <CallsTab v-else-if="ui.tab === 'calls'" />
        <ContactsTab v-else />
      </main>

      <p v-if="ui.engineFailed" class="notice flex items-center gap-1.5 text-error" role="alert">
        <Icon name="alert" />The phone engine stopped; restart the application.
      </p>
      <p v-else-if="notice" class="notice flex items-center gap-1.5" role="status">
        <Icon name="alert" />{{ notice }}
      </p>
    </template>

    <StatusBar />

    <!-- One dialog at a time: a ringing call first, then an agent's request, then a confirmation. -->
    <Dialog v-if="incoming" title="Incoming call" @cancel="ui.dispatch({ type: 'end' })">
      <p class="flex items-center gap-1.5 font-semibold text-ringing">
        <Icon name="bell" />Incoming call
      </p>
      <!-- The caller's name is remote-controlled: interpolation only. -->
      <p class="truncate text-[1.4rem] font-semibold">
        {{ nameOf(incoming.remote) ?? incoming.remote }}
      </p>
      <p v-if="nameOf(incoming.remote)" class="num text-muted">{{ incoming.remote }}</p>
      <p>
        <span v-if="incoming.secure" class="badge text-secure"
          ><Icon name="lock" />Encrypted (SRTP)</span
        >
        <span v-else class="badge text-insecure"><Icon name="unlock" />Unencrypted (RTP)</span>
      </p>
      <div class="grid grid-cols-2 gap-1.5 pt-1">
        <button
          class="btn btn-call min-h-[2.6rem]"
          autofocus
          @click="ui.dispatch({ type: 'call' })"
        >
          <Icon name="phone" />Answer
        </button>
        <button class="btn btn-hangup min-h-[2.6rem]" @click="ui.dispatch({ type: 'end' })">
          <Icon name="hangup" />Reject
        </button>
      </div>
    </Dialog>

    <Dialog
      v-else-if="approval"
      :key="approval.id"
      title="Agent request"
      @cancel="ui.dispatch({ type: 'deny', id: approval.id })"
    >
      <!-- Requester and description come from another program: interpolation only. -->
      <p class="font-semibold">{{ approval.requester }} asks to:</p>
      <p class="well px-2 py-1 font-semibold text-held">{{ approval.description }}</p>
      <p class="text-[0.9rem] text-muted">
        Expires in {{ Math.max(0, Math.ceil((approval.deadline - now) / 1000)) }} s
        <template v-if="ui.approvals.length > 1">
          · {{ ui.approvals.length - 1 }} more waiting</template
        >
      </p>
      <div class="grid grid-cols-2 gap-1.5">
        <button class="btn" autofocus @click="ui.dispatch({ type: 'deny', id: approval.id })">
          <Icon name="close" />Deny
        </button>
        <button
          class="btn"
          :disabled="!armed"
          :title="armed ? undefined : 'Available after one second on screen'"
          @click="ui.dispatch({ type: 'approve', id: approval.id })"
        >
          <Icon name="check" />Allow
        </button>
      </div>
      <button
        class="btn"
        title="Deny every waiting request and pause new ones"
        @click="ui.dispatch({ type: 'denyAll' })"
      >
        Deny all and pause
      </button>
    </Dialog>

    <Dialog v-else-if="ui.confirm" title="Confirm" @cancel="ui.dispatch({ type: 'cancelConfirm' })">
      <p>{{ confirmQuestion(ui.confirm) }}</p>
      <div class="flex justify-end gap-1.5">
        <button class="btn" autofocus @click="ui.dispatch({ type: 'cancelConfirm' })">
          Cancel
        </button>
        <button class="btn btn-hangup" @click="ui.dispatch({ type: 'confirm' })">
          {{ confirmButton(ui.confirm) }}
        </button>
      </div>
    </Dialog>
  </div>
</template>
