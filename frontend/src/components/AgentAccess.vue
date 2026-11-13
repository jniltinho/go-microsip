<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import {
  type AuditEntry,
  auditLog,
  clearAudit,
  createGrant,
  type Grant,
  GRANT_SCOPES,
  grants as listGrants,
  revokeGrant,
} from "../api";
import Icon from "./Icon.vue";

// Settings, Agent access: what a program (the command line, an MCP agent) may do without asking
// each time. A grant is a secret written once to a private file; nothing here shows it.
const form = reactive({
  scopes: [] as string[],
  destinations: "",
  incoming: "",
  minutes: 15,
  quota: 1,
});
const list = ref<Grant[]>([]);
const written = ref("");
const failure = ref("");
const log = ref<AuditEntry[]>();
const clearing = ref(false);

const numbers = (text: string) =>
  text
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);

async function refresh() {
  try {
    list.value = await listGrants();
    if (log.value) log.value = await auditLog();
    failure.value = "";
  } catch (e) {
    failure.value = String(e);
  }
}
onMounted(refresh);

async function create(bootstrap: boolean) {
  written.value = "";
  try {
    written.value = await createGrant(
      {
        scopes: [...form.scopes],
        destinations: numbers(form.destinations),
        incoming: numbers(form.incoming),
        minutes: Number(form.minutes),
        quota: Number(form.quota),
      },
      bootstrap,
    );
    await refresh();
  } catch (e) {
    failure.value = String(e);
  }
}

async function revoke(id: string) {
  // A revocation that was not stored is said after the list is read again, which clears the
  // message otherwise: an export for the headless service is then still listed, and usable.
  const refused = await revokeGrant(id).then(() => "", String);
  await refresh();
  if (refused) failure.value = refused;
}

async function toggleLog() {
  clearing.value = false;
  log.value = log.value ? undefined : await auditLog().catch(() => []);
}

async function clear() {
  await clearAudit().catch((e: unknown) => (failure.value = String(e)));
  clearing.value = false;
  log.value = await auditLog().catch(() => []);
}

const minutesLeft = (g: Grant) => Math.max(0, Math.ceil((g.expires - Date.now()) / 60_000));
const time = (at: number) => new Date(at).toLocaleTimeString([], { hour12: false });
</script>

<template>
  <fieldset class="group flex flex-col gap-1.5" data-testid="agent-access">
    <legend>Agent access</legend>
    <p class="text-[0.9rem] text-muted">
      A program you start (command line, MCP agent) may do what you tick without asking each time.
      Anything else still asks here.
    </p>
    <div class="grid grid-cols-2 gap-x-3 gap-y-1" role="group" aria-label="Permissions">
      <label v-for="scope in GRANT_SCOPES" :key="scope.id" class="flex items-center gap-1.5">
        <input v-model="form.scopes" type="checkbox" :value="scope.id" />
        {{ scope.label }}
      </label>
    </div>
    <label>
      <span class="label">May call (exact numbers, comma-separated)</span>
      <input v-model="form.destinations" class="field" autocomplete="off" spellcheck="false" />
    </label>
    <label>
      <span class="label">May answer (exact callers, comma-separated)</span>
      <input v-model="form.incoming" class="field" autocomplete="off" spellcheck="false" />
    </label>
    <div class="grid grid-cols-2 gap-x-2">
      <label>
        <span class="label">Valid for (minutes, 1–60)</span>
        <input v-model="form.minutes" class="field" type="number" min="1" max="60" />
      </label>
      <label>
        <span class="label">Calls allowed (0–100)</span>
        <input v-model="form.quota" class="field" type="number" min="0" max="100" />
      </label>
    </div>
    <div class="flex flex-col gap-1.5">
      <button class="btn" type="button" @click="create(false)">
        <Icon name="plus" />Create grant file
      </button>
      <button
        class="btn"
        type="button"
        title="A single-use grant that starts go-microsip service on a machine without a desktop"
        @click="create(true)"
      >
        <Icon name="out" />Export for headless service
      </button>
    </div>
    <p v-if="failure" class="flex items-center gap-1 text-error" role="alert">
      <Icon name="alert" />{{ failure }}
    </p>
    <p v-if="written" class="well px-2 py-1 break-all" role="status">
      Grant created. Pass its file with --grant-file:<br />{{ written }}
    </p>

    <p class="label">Grants in use</p>
    <p v-if="!list.length" class="text-muted">None.</p>
    <ul v-else class="flex flex-col gap-1">
      <li v-for="g in list" :key="g.id" class="well flex items-center gap-1.5 px-2 py-1">
        <span class="min-w-0 flex-1">
          <span class="block truncate font-semibold">
            {{ g.bootstrap ? "Headless service" : "This session" }} · {{ g.scopes.length }}
            {{ g.scopes.length === 1 ? "permission" : "permissions" }}
          </span>
          <span class="block text-[0.9rem] text-muted">
            {{ minutesLeft(g) }} min left · calls {{ g.used }}/{{ g.quota }}
            <template v-if="g.destinations.length">
              · may call {{ g.destinations.join(", ") }}</template
            >
          </span>
        </span>
        <button
          class="btn"
          type="button"
          :aria-label="`Revoke grant ${g.id}`"
          @click="revoke(g.id)"
        >
          <Icon name="close" />Revoke
        </button>
      </li>
    </ul>

    <div class="grid grid-cols-2 gap-1.5">
      <button class="btn" type="button" :aria-expanded="!!log" @click="toggleLog">
        <Icon name="wait" />{{ log ? "Hide audit log" : "Show audit log" }}
      </button>
      <button
        v-if="log && !clearing"
        class="btn"
        type="button"
        :disabled="!log.length"
        @click="clearing = true"
      >
        <Icon name="trash" />Clear audit log
      </button>
    </div>
    <div
      v-if="clearing"
      class="well flex flex-col gap-1.5 px-2 py-1"
      role="alertdialog"
      aria-label="Clear audit log"
    >
      <p>Delete the whole record of agent requests and decisions?</p>
      <div class="grid grid-cols-2 gap-1.5">
        <button class="btn" type="button" @click="clearing = false">
          <Icon name="close" />Cancel
        </button>
        <button class="btn" type="button" @click="clear"><Icon name="trash" />Delete</button>
      </div>
    </div>
    <template v-if="log">
      <p v-if="!log.length" class="text-muted">The audit log is empty.</p>
      <ul
        v-else
        class="well max-h-40 overflow-y-auto px-2 py-1 text-[0.9rem]"
        aria-label="Audit log"
      >
        <li v-for="(entry, i) in log" :key="i">
          <span class="num">{{ time(entry.at) }}</span> {{ entry.operation }}: {{ entry.decision }}
          <template v-if="entry.detail !== '-'"> ({{ entry.detail }})</template>
        </li>
      </ul>
    </template>
  </fieldset>
</template>
