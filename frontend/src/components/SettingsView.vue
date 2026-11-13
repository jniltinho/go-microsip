<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from "vue";
import {
  type AudioDevices,
  audioDevices,
  audioTest,
  type MediaSecurity,
  NOTICES,
  type Notice,
  saveSettings,
  settings,
  type SipTransport,
  startAudioTest,
  stopAudioTest,
} from "../api";
import { defaultPort, noticeText } from "../state";
import { about, ui } from "../store";
import AgentAccess from "./AgentAccess.vue";
import Icon from "./Icon.vue";
import SkinPicker from "./SkinPicker.vue";

// What the Rust candidate offers and this contract does not carry yet: listed, never faked.
const MISSING = ["Speaker and microphone volume"];

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

const fail = (e: unknown) =>
  ui.setNotice(NOTICES.includes(e as Notice) ? noticeText(e as Notice) : String(e), Date.now());

// The sound test: a tone on the chosen speaker and the level of the chosen microphone. It ends by
// itself, when a call begins, and when the settings are left.
const test = reactive({ running: false, level: 0 });
let watching: ReturnType<typeof setInterval> | undefined;
let gone = false; // the settings were left
function watched() {
  clearInterval(watching);
  watching = undefined;
  test.running = false;
  test.level = 0;
}
async function toggleTest() {
  if (test.running) {
    watched();
    return stopAudioTest().catch(fail);
  }
  try {
    await startAudioTest();
  } catch (e) {
    return fail(e);
  }
  // The settings were left while the devices were opening: leaving stopped that start, and what
  // runs now, if anything, is another page's.
  if (gone) return;
  test.running = true;
  watching = setInterval(async () => {
    const t = await audioTest().catch(() => ({ running: false, level: 0 }));
    if (!watching) return; // stopped while the answer was on its way
    if (t.running) test.level = t.level;
    else watched();
  }, 100);
}
onUnmounted(() => {
  gone = true;
  void stopAudioTest().catch(() => {}); // also one that is still starting
  watched();
});

// The test is of the devices it started on: another choice ends it.
async function saveAudio() {
  if (test.running) await toggleTest();
  await saveSettings(audio).catch(fail);
}
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
          <p class="text-[0.9rem] text-muted">
            Used by the next call; a call in progress keeps its devices.
          </p>
          <div class="flex items-center gap-1.5">
            <button
              class="btn"
              type="button"
              :disabled="!devices || !!ui.call"
              :title="ui.call ? 'Not during a call' : undefined"
              @click="toggleTest"
            >
              <Icon :name="test.running ? 'close' : 'play'" />{{
                test.running ? "Stop test" : "Test audio"
              }}
            </button>
            <label v-if="test.running" class="flex flex-1 items-center gap-1.5">
              <span class="label">Microphone level</span>
              <meter class="flex-1" min="0" max="1" :value="test.level"></meter>
              <span class="tabular-nums">{{ Math.round(test.level * 100) }} %</span>
            </label>
          </div>
          <p v-if="test.running" class="text-[0.9rem] text-muted" role="status">
            A tone is playing on the speaker; speak to see the microphone's level.
          </p>
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
