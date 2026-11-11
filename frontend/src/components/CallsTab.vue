<script setup lang="ts">
import { ref, watch } from "vue";
import { history, type HistoryEntry, type HistoryFilter } from "../api";
import { moveFocus } from "../keys";
import { ageText, formatDuration, outcomeText } from "../state";
import { nameOf, now, ui } from "../store";
import Icon, { type IconName } from "./Icon.vue";

const FILTERS: { id: HistoryFilter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "incoming", label: "In" },
  { id: "outgoing", label: "Out" },
  { id: "missed", label: "Missed" },
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

// Direction and outcome are told by the icon's shape and the outcome label, not by color.
const icon = (e: HistoryEntry): IconName =>
  e.outcome === "missed" ? "missed" : e.direction === "incoming" ? "in" : "out";
const tone = (e: HistoryEntry) =>
  e.outcome === "answered"
    ? "text-active"
    : e.outcome === "missed" || e.outcome === "failed"
      ? "text-error"
      : "text-muted";
</script>

<template>
  <section class="flex h-full flex-col gap-1.5 p-2" aria-label="Calls">
    <div class="grid grid-cols-4 gap-1" role="group" aria-label="Show">
      <button
        v-for="f in FILTERS"
        :key="f.id"
        class="btn"
        :aria-pressed="filter === f.id"
        @click="filter = f.id"
      >
        {{ f.label }}
      </button>
    </div>
    <p v-if="failure" class="well flex-1 p-3 text-error" role="alert">
      <Icon name="alert" /> Call history unavailable: {{ failure }}
    </p>
    <ul
      v-else
      class="well min-h-0 flex-1 overflow-auto"
      aria-label="Call history"
      @keydown="moveFocus($event, '.row')"
    >
      <li v-for="e in entries" :key="e.id">
        <button
          class="row"
          :aria-current="selected === e.id"
          :aria-expanded="selected === e.id"
          @click="selected = selected === e.id ? undefined : e.id"
        >
          <Icon :name="icon(e)" :class="tone(e)" />
          <span class="min-w-0 flex-1">
            <span class="block truncate font-semibold">{{ nameOf(e.remote) ?? e.remote }}</span>
            <span class="block truncate text-[0.9rem] text-muted">
              {{ e.direction === "incoming" ? "In" : "Out" }} · {{ outcomeText(e.outcome) }} ·
              {{ e.remote }}
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
      <li v-if="!entries.length" class="p-3 text-muted">
        {{ search || filter !== "all" ? "No calls match." : "No calls yet." }}
      </li>
    </ul>
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
