<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { history, type HistoryEntry, type HistoryFilter } from "../api";
import { moveFocus } from "../keys";
import { ageText, formatDuration, outcomeText } from "../state";
import { nameOf, now, ui } from "../store";
import Icon, { type IconName } from "./Icon.vue";

const FILTERS: { id: HistoryFilter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "missed", label: "Missed" },
  { id: "incoming", label: "Incoming" },
  { id: "outgoing", label: "Outgoing" },
];
const filter = ref<HistoryFilter>("all");
const search = ref("");
const entries = ref<HistoryEntry[]>([]);
const failure = ref("");
const selected = ref<number>();

watch(
  [filter, search, () => ui.historyRevision],
  async () => {
    try {
      entries.value = await history(filter.value, search.value);
      failure.value = "";
    } catch (e) {
      entries.value = [];
      failure.value = String(e);
    }
  },
  { immediate: true },
);

// Direction and outcome are told by the icon's shape and the words beside it, not by color.
const icon = (e: HistoryEntry): IconName =>
  e.outcome === "missed" ? "missed" : e.direction === "incoming" ? "in" : "out";

/** The entries (newest first) under the day they started: Today, Yesterday, then the date. */
const days = computed(() => {
  const midnight = new Date(now.value).setHours(0, 0, 0, 0) / 1000;
  const groups: { label: string; entries: HistoryEntry[] }[] = [];
  for (const e of entries.value) {
    const label =
      e.startedUtc >= midnight
        ? "Today"
        : e.startedUtc >= midnight - 86400
          ? "Yesterday"
          : new Date(e.startedUtc * 1000).toLocaleDateString("en", { dateStyle: "medium" });
    if (groups.at(-1)?.label === label) groups.at(-1)?.entries.push(e);
    else groups.push({ label, entries: [e] });
  }
  return groups;
});
</script>

<template>
  <section class="flex h-full flex-col gap-2 px-3 pb-3" aria-label="Calls">
    <div class="flex flex-wrap gap-1.5" role="group" aria-label="Show">
      <button
        v-for="f in FILTERS"
        :key="f.id"
        class="btn pill toggle"
        :aria-pressed="filter === f.id"
        @click="filter = f.id"
      >
        <Icon v-if="filter === f.id" name="check" />{{ f.label }}
      </button>
    </div>
    <p v-if="failure" class="well flex-1 p-3 text-error" role="alert">
      <Icon name="alert" /> Call history unavailable: {{ failure }}
    </p>
    <div
      v-else
      class="flex min-h-0 flex-1 flex-col gap-1.5 overflow-auto"
      role="group"
      aria-label="Call history"
      @keydown="moveFocus($event, '.row')"
    >
      <template v-for="day in days" :key="day.label">
        <h2 class="label mb-0 font-semibold">{{ day.label }}</h2>
        <ul class="well shrink-0 overflow-hidden" :aria-label="day.label">
          <li v-for="e in day.entries" :key="e.id">
            <button
              class="row"
              :aria-current="selected === e.id"
              :aria-expanded="selected === e.id"
              @click="selected = selected === e.id ? undefined : e.id"
            >
              <span
                class="avatar w-[2.5rem]"
                :class="e.outcome === 'missed' ? 'bg-error-soft text-error' : ''"
                ><Icon :name="icon(e)"
              /></span>
              <span class="min-w-0 flex-1">
                <span class="block truncate font-semibold">{{ nameOf(e.remote) ?? e.remote }}</span>
                <span class="block truncate text-[0.9rem] text-muted">
                  <span v-if="e.outcome === 'missed'" class="font-semibold text-error">Missed</span>
                  <template v-else>
                    {{ e.direction === "incoming" ? "Incoming" : "Outgoing" }} ·
                    {{ outcomeText(e.outcome) }}
                  </template>
                  · {{ e.remote }}
                </span>
              </span>
              <span class="num text-right text-[0.9rem] text-muted">
                <span class="block">{{ ageText(now / 1000 - e.startedUtc) }}</span>
                <span class="block">{{
                  e.durationMs === undefined ? "—" : formatDuration(e.durationMs)
                }}</span>
              </span>
            </button>
            <div
              v-if="selected === e.id"
              class="grid grid-cols-4 gap-1 border-b border-line-subtle p-1"
            >
              <button
                class="btn px-1"
                :disabled="!!ui.call"
                @click="ui.dispatch({ type: 'dialNumber', number: e.remote })"
              >
                Call
              </button>
              <button
                class="btn px-1"
                title="Put the number in the dialpad"
                @click="ui.dispatch({ type: 'fillNumber', number: e.remote })"
              >
                Edit
              </button>
              <button
                class="btn px-1"
                :disabled="!!nameOf(e.remote)"
                :title="nameOf(e.remote) ? 'Already a contact' : 'Add to contacts'"
                @click="ui.dispatch({ type: 'newContact', number: e.remote })"
              >
                Add
              </button>
              <button class="btn px-1" @click="ui.dispatch({ type: 'deleteCall', id: e.id })">
                Delete
              </button>
            </div>
          </li>
        </ul>
      </template>
      <p v-if="!entries.length" class="well p-3 text-muted">
        {{ search || filter !== "all" ? "No calls match." : "No calls yet." }}
      </p>
    </div>
    <div class="flex gap-1">
      <input
        v-model="search"
        class="field"
        type="search"
        placeholder="Search calls"
        aria-label="Search calls"
      />
      <button
        class="btn"
        :disabled="!entries.length"
        @click="ui.dispatch({ type: 'clearHistory' })"
      >
        Clear all
      </button>
    </div>
  </section>
</template>
