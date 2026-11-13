<script setup lang="ts">
// Home: who to call. A search by name or number, the favorites, and the most recent calls, each
// one click from a call.
import { computed, onMounted, ref, watch } from "vue";
import { history, type HistoryEntry } from "../api";
import { moveFocus } from "../keys";
import { ageText } from "../state";
import { book, initials, nameOf, now, ui } from "../store";
import Icon from "./Icon.vue";

const query = ref("");
const recent = ref<HistoryEntry[]>([]);
const failure = ref("");
const results = ref<HTMLElement>();
const search = ref<HTMLInputElement>();
// Typing works at once, except when the arrow keys are walking the navigation through Home.
onMounted(() => {
  if (!document.activeElement?.closest(".navbar")) search.value?.focus();
});

watch(
  () => ui.historyRevision,
  async () => {
    try {
      recent.value = await history("all", "");
      failure.value = "";
    } catch (e) {
      recent.value = [];
      failure.value = String(e);
    }
  },
  { immediate: true },
);

// The first four favorites fit the row at the smallest window; Contacts lists them all, first.
const favorites = computed(() =>
  book.value.filter((c) => c.favorite && c.numbers.length).slice(0, 4),
);
const matches = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return [];
  return book.value.filter(
    (c) =>
      c.numbers.length &&
      (c.name.toLowerCase().includes(q) || c.numbers.some((n) => n.number.includes(q))),
  );
});
const call = (number: string) => ui.dispatch({ type: "dialNumber", number });

// Enter dials what was typed when it is a number or matches nobody (the reducer validates it),
// calls the only contact a name matches, and otherwise moves to the list to choose.
function submit() {
  const q = query.value.trim();
  if (!q) return;
  if (/^[\d+*#\s().-]+$/.test(q) || !matches.value.length) void call(q);
  else if (matches.value.length === 1) void call(matches.value[0].numbers[0].number);
  else results.value?.querySelector<HTMLElement>("button")?.focus();
}
</script>

<template>
  <section class="pane flex h-full flex-col gap-2.5 px-3 pb-3" aria-label="Home">
    <div class="relative">
      <Icon name="search" class="absolute top-1/2 left-4 -translate-y-1/2 text-muted" />
      <input
        ref="search"
        v-model="query"
        class="field pill min-h-[3.7rem] pl-10 text-[1.1rem]"
        type="search"
        maxlength="64"
        placeholder="Name or number"
        aria-label="Name or number"
        autocomplete="off"
        spellcheck="false"
        @keydown.enter.prevent="submit"
      />
    </div>

    <ul
      v-if="query.trim()"
      ref="results"
      class="well min-h-0 flex-1 overflow-auto"
      aria-label="Matching contacts"
      @keydown="moveFocus($event, '.btn-call')"
    >
      <li v-for="c in matches" :key="c.id" class="row">
        <span class="avatar" aria-hidden="true">{{ initials(c.name) }}</span>
        <!-- Contact fields may come from an import: interpolation only. -->
        <span class="min-w-0 flex-1">
          <span class="block truncate font-semibold">{{ c.name }}</span>
          <span class="num block truncate text-[0.9rem] text-muted">{{ c.numbers[0].number }}</span>
        </span>
        <button
          class="btn btn-call pill w-[3.1rem] min-h-[3.1rem] shrink-0 p-0"
          :aria-label="`Call ${c.name}`"
          @click="call(c.numbers[0].number)"
        >
          <Icon name="phone" />
        </button>
      </li>
      <li v-if="!matches.length" class="p-3 text-muted">
        No contact matches. Press Enter to call "{{ query.trim() }}".
      </li>
    </ul>

    <template v-else>
      <template v-if="favorites.length">
        <h2 class="label mb-0 font-semibold">Favorites</h2>
        <div
          class="grid shrink-0 grid-cols-4 gap-1"
          role="group"
          aria-label="Favorites"
          @keydown="moveFocus($event, '.fav', true)"
        >
          <button
            v-for="c in favorites"
            :key="c.id"
            class="fav flex min-w-0 flex-col items-center gap-1 p-1"
            :aria-label="`Call ${c.name}`"
            @click="call(c.numbers[0].number)"
          >
            <span class="avatar w-[3.7rem] text-[1.2rem]" aria-hidden="true">{{
              initials(c.name)
            }}</span>
            <span class="w-full truncate text-[0.9rem]">{{ c.name.split(/\s+/)[0] }}</span>
          </button>
        </div>
      </template>

      <h2 class="label mb-0 font-semibold">Recent</h2>
      <p v-if="failure" class="well flex-1 p-3 text-error" role="alert">
        <Icon name="alert" /> Call history unavailable: {{ failure }}
      </p>
      <ul
        v-else
        class="well min-h-0 flex-1 overflow-auto"
        aria-label="Recent calls"
        @keydown="moveFocus($event, '.btn-call')"
      >
        <li v-for="e in recent" :key="e.id" class="row">
          <span class="avatar" aria-hidden="true">{{ initials(nameOf(e.remote) ?? "") }}</span>
          <span class="min-w-0 flex-1">
            <!-- Remote-controlled text: interpolation only. -->
            <span class="block truncate font-semibold">{{ nameOf(e.remote) ?? e.remote }}</span>
            <span class="flex items-center gap-1 text-[0.9rem] text-muted">
              <template v-if="e.outcome === 'missed'">
                <Icon name="missed" class="text-error" />
                <span class="font-semibold text-error">Missed</span>
              </template>
              <template v-else>
                <Icon :name="e.direction === 'incoming' ? 'in' : 'out'" />
                {{ e.direction === "incoming" ? "Incoming" : "Outgoing" }}
              </template>
              · {{ ageText(now / 1000 - e.startedUtc) }}
            </span>
          </span>
          <button
            class="btn btn-call pill w-[3.1rem] min-h-[3.1rem] shrink-0 p-0"
            :aria-label="`Call ${nameOf(e.remote) ?? e.remote}`"
            @click="call(e.remote)"
          >
            <Icon name="phone" />
          </button>
        </li>
        <li v-if="!recent.length" class="p-3 text-muted">No calls yet.</li>
      </ul>
    </template>
  </section>
</template>
