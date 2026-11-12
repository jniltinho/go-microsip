<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import {
  type AudioDevices,
  audioDevices,
  type MediaSecurity,
  saveSettings,
  settings,
  type SipTransport,
} from "../api";
import { defaultPort } from "../state";
import { about, ui } from "../store";
import AgentAccess from "./AgentAccess.vue";
import Icon from "./Icon.vue";
import SkinPicker from "./SkinPicker.vue";

// What the Rust candidate offers and this contract does not carry yet: listed, never faked.
const MISSING = ["Audio test", "Speaker and microphone volume", "Contact CSV import"];

const a = ui.account;
const form = reactive({
  displayName: a?.displayName ?? "",
  username: a?.username ?? "",
  authUsername: a?.authUsername ?? "",
  domain: a?.domain ?? "",
  server: a?.server ?? "",
  port: String(a?.port ?? defaultPort("udp")),
  transport: (a?.transport ?? "udp") as SipTransport,
  media: (a?.media ?? "rtp") as MediaSecurity,
  password: "",
});
const devices = ref<AudioDevices>();
const devicesFailure = ref("");
const audio = reactive({ audioInput: "", audioOutput: "" });

onMounted(async () => {
  try {
    const s = await settings();
    audio.audioInput = s.audioInput;
    audio.audioOutput = s.audioOutput;
    devices.value = await audioDevices();
  } catch (e) {
    devicesFailure.value = String(e);
  }
});

// Changing the transport moves the port only when it still is the old default.
function setTransport(next: SipTransport) {
  if (form.port.trim() === String(defaultPort(form.transport)))
    form.port = String(defaultPort(next));
  form.transport = next;
}

async function save() {
  const port = /^\d{1,5}$/.test(form.port.trim()) ? Number(form.port) : 0;
  await ui.dispatch({
    type: "saveAccount",
    account: {
      displayName: form.displayName.trim(),
      username: form.username.trim(),
      authUsername: form.authUsername.trim(),
      domain: form.domain.trim(),
      server: form.server.trim(),
      port,
      transport: form.transport,
      media: form.media,
      expires: a?.expires ?? 300,
    },
    password: form.password || null,
  });
  form.password = "";
}

const saveAudio = () =>
  saveSettings(audio).catch((e: unknown) => ui.setNotice(String(e), Date.now()));
</script>

<template>
  <section class="flex h-full flex-col" aria-label="Settings">
    <div class="titlebar">
      <span class="titlebar-text">Settings</span>
      <button class="btn" aria-label="Close settings" @click="ui.dispatch({ type: 'closeView' })">
        <Icon name="close" />
      </button>
    </div>
    <div class="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-2">
      <form @submit.prevent="save">
        <fieldset class="group grid grid-cols-2 gap-x-2 gap-y-1.5">
          <legend>Account</legend>
          <label class="col-span-2">
            <span class="label">Display name</span>
            <input v-model="form.displayName" class="field" maxlength="64" autocomplete="off" />
          </label>
          <label>
            <span class="label">Username</span>
            <input
              v-model="form.username"
              class="field"
              maxlength="64"
              autocomplete="off"
              spellcheck="false"
              v-focus
            />
          </label>
          <label>
            <span class="label">Login (optional)</span>
            <input
              v-model="form.authUsername"
              class="field"
              maxlength="64"
              autocomplete="off"
              spellcheck="false"
            />
          </label>
          <label class="col-span-2">
            <span class="label">Domain</span>
            <input
              v-model="form.domain"
              class="field"
              maxlength="253"
              autocomplete="off"
              spellcheck="false"
            />
          </label>
          <label>
            <span class="label">Server</span>
            <input
              v-model="form.server"
              class="field"
              maxlength="253"
              autocomplete="off"
              spellcheck="false"
            />
          </label>
          <label>
            <span class="label">Port</span>
            <input
              v-model="form.port"
              class="field num"
              maxlength="5"
              inputmode="numeric"
              autocomplete="off"
            />
          </label>
          <label>
            <span class="label">Transport</span>
            <select
              class="field"
              :value="form.transport"
              @change="setTransport(($event.target as HTMLSelectElement).value as SipTransport)"
            >
              <option value="udp">UDP</option>
              <option value="tcp">TCP</option>
              <option value="tls">TLS</option>
            </select>
          </label>
          <label>
            <span class="label">Media</span>
            <select v-model="form.media" class="field">
              <option value="rtp">RTP (unencrypted)</option>
              <option value="srtp">SRTP (needs TLS)</option>
            </select>
          </label>
          <label class="col-span-2">
            <span class="label">Password</span>
            <input
              v-model="form.password"
              class="field"
              type="password"
              autocomplete="off"
              :placeholder="ui.hasPassword ? 'Unchanged' : ''"
            />
          </label>
          <div class="col-span-2 flex justify-end gap-1 pt-1">
            <button
              class="btn"
              type="button"
              :disabled="ui.registration.state === 'disabled'"
              @click="ui.dispatch({ type: 'unregister' })"
            >
              Unregister
            </button>
            <button class="btn" type="submit"><Icon name="check" />Save and register</button>
          </div>
        </fieldset>
      </form>

      <fieldset class="group flex flex-col gap-1.5">
        <legend>Audio</legend>
        <p v-if="devicesFailure" class="flex items-center gap-1 text-error" role="alert">
          <Icon name="alert" />Audio devices unavailable: {{ devicesFailure }}
        </p>
        <template v-else>
          <label>
            <span class="label">Microphone</span>
            <select
              v-model="audio.audioInput"
              class="field"
              :disabled="!devices"
              @change="saveAudio"
            >
              <option value="">System default</option>
              <option v-for="d in devices?.inputs" :key="d.id" :value="d.id">{{ d.name }}</option>
            </select>
          </label>
          <label>
            <span class="label">Speaker</span>
            <select
              v-model="audio.audioOutput"
              class="field"
              :disabled="!devices"
              @change="saveAudio"
            >
              <option value="">System default</option>
              <option v-for="d in devices?.outputs" :key="d.id" :value="d.id">{{ d.name }}</option>
            </select>
          </label>
          <p class="text-[0.9rem] text-muted">Applied on the next start.</p>
        </template>
      </fieldset>

      <fieldset class="group">
        <legend>Appearance</legend>
        <SkinPicker />
      </fieldset>

      <fieldset class="group">
        <legend>Phone</legend>
        <label class="flex items-center gap-1.5">
          <input type="checkbox" :checked="ui.dnd" @change="ui.dispatch({ type: 'toggleDnd' })" />
          Do not disturb (new calls get a busy answer)
        </label>
      </fieldset>

      <AgentAccess />

      <fieldset class="group">
        <legend>Not available in this build</legend>
        <ul class="text-muted">
          <li v-for="m in MISSING" :key="m" class="flex items-center gap-1.5">
            <Icon name="off" />{{ m }}
          </li>
        </ul>
      </fieldset>

      <p class="text-[0.9rem] text-muted" data-testid="version">{{ about }}</p>
    </div>
  </section>
</template>
