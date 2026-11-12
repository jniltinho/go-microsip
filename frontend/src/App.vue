<script setup lang="ts">
// The compact softphone window (layout B of docs/ui/layouts): the header with the account and the
// registration state, the current destination, notices, the bottom navigation and the dialogs.
// Everything it shows about the engine comes from store.ui, which changes only on backend events.
import { computed, nextTick, onBeforeUnmount, onMounted, watch } from "vue";
import AppMenu from "./components/AppMenu.vue";
import CallsTab from "./components/CallsTab.vue";
import ContactEditor from "./components/ContactEditor.vue";
import ContactsTab from "./components/ContactsTab.vue";
import Dialog from "./components/Dialog.vue";
import DialpadTab from "./components/DialpadTab.vue";
import HomeTab from "./components/HomeTab.vue";
import Icon, { type IconName } from "./components/Icon.vue";
import SettingsView from "./components/SettingsView.vue";
import SkinPicker from "./components/SkinPicker.vue";
import StatusBar from "./components/StatusBar.vue";
import { moveFocus } from "./keys";
import { confirmButton, confirmQuestion, phaseText, type Tab, TABS } from "./state";
import { about, initials, nameOf, now, start, ui, unavailable } from "./store";

const TAB_ICONS: Record<Tab, IconName> = {
  home: "home",
  calls: "wait",
  contacts: "user",
  phone: "keypad",
};

const notice = computed(() => ui.notice(now.value));
const incoming = computed(() => (ui.incomingRinging ? ui.call?.snapshot : undefined));
const approval = computed(() => ui.approvals[0]);
const armed = computed(() => ui.approvalArmed(now.value));

// A call that was dialed or answered takes the window, unless a form is open (its edits stay).
watch(
  () => !!ui.call && !ui.incomingRinging,
  (active) => {
    if (active && ui.view === "main") void ui.dispatch({ type: "selectTab", tab: "phone" });
  },
);

// Keyboard: Ctrl+L number entry, Enter call/answer, Escape hang up/reject or leave a view, and on
// the keypad digits, * and # type the number or send DTMF. Dialogs and menus handle their own keys.
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
    <StatusBar>
      <template v-if="!unavailable && !ui.stopped">
        <button
          class="btn pill toggle"
          :aria-pressed="ui.dnd"
          title="Do not disturb"
          @click="ui.dispatch({ type: 'toggleDnd' })"
        >
          <Icon :name="ui.dnd ? 'check' : 'dnd'" />DND
        </button>
        <AppMenu />
      </template>
    </StatusBar>

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
        <HomeTab v-else-if="ui.tab === 'home'" />
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

      <nav
        class="navbar"
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
          <span class="marker"><Icon :name="TAB_ICONS[t.id]" /></span>{{ t.label }}
        </button>
      </nav>
    </template>

    <!-- One dialog at a time: a ringing call first, then an agent's request, then a confirmation. -->
    <Dialog v-if="incoming" title="Incoming call" sheet @cancel="ui.dispatch({ type: 'end' })">
      <p class="text-center">
        <span class="badge bg-ringing-soft text-ringing"><Icon name="bell" />Incoming call</span>
      </p>
      <span class="avatar avatar-big mx-auto" aria-hidden="true">{{
        initials(nameOf(incoming.remote) ?? "")
      }}</span>
      <!-- The caller's name is remote-controlled: interpolation only. -->
      <p class="truncate text-center text-[1.8rem] leading-tight font-semibold">
        {{ nameOf(incoming.remote) ?? incoming.remote }}
      </p>
      <p v-if="nameOf(incoming.remote)" class="num text-center text-[1.1rem] text-muted">
        {{ incoming.remote }}
      </p>
      <p class="text-center">
        <span v-if="incoming.secure" class="badge bg-secure-soft text-secure"
          ><Icon name="lock" />Encrypted (SRTP)</span
        >
        <span v-else class="badge bg-insecure-soft text-insecure"
          ><Icon name="unlock" />Unencrypted (RTP)</span
        >
      </p>
      <div class="grid grid-cols-2 gap-3 pt-1">
        <button class="btn btn-hangup pill big" @click="ui.dispatch({ type: 'end' })">
          <Icon name="hangup" />Reject
        </button>
        <button class="btn btn-call pill big" autofocus @click="ui.dispatch({ type: 'call' })">
          <Icon name="phone" />Answer
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
        <button
          class="btn"
          :class="ui.confirm.kind === 'dial' ? 'btn-call' : 'btn-hangup'"
          @click="ui.dispatch({ type: 'confirm' })"
        >
          {{ confirmButton(ui.confirm) }}
        </button>
      </div>
    </Dialog>
  </div>
</template>
