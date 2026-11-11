<script setup lang="ts">
import { ref, watch } from "vue";
import { type Contact, contacts, saveContact } from "../api";
import { moveFocus } from "../keys";
import { ui } from "../store";
import Icon from "./Icon.vue";

const search = ref("");
const list = ref<Contact[]>([]);
const failure = ref("");
const selected = ref<number>();

watch(
  [search, () => ui.contactsRevision],
  async () => {
    try {
      list.value = await contacts(search.value);
      failure.value = "";
    } catch (e) {
      list.value = [];
      failure.value = String(e);
    }
  },
  { immediate: true },
);

async function toggleFavorite(c: Contact) {
  try {
    await saveContact({ ...c, favorite: !c.favorite });
    ui.contactsRevision++;
  } catch (e) {
    ui.setNotice(String(e), Date.now());
  }
}
</script>

<template>
  <section class="flex h-full flex-col gap-1.5 p-2" aria-label="Contacts">
    <p v-if="failure" class="well flex-1 p-3 text-error" role="alert">
      <Icon name="alert" /> Contacts unavailable: {{ failure }}
    </p>
    <ul
      v-else
      class="well min-h-0 flex-1 overflow-auto"
      aria-label="Contact list"
      @keydown="moveFocus($event, '.row')"
    >
      <li v-for="c in list" :key="c.id">
        <div class="flex border-b border-line-subtle">
          <button
            class="px-2"
            :class="c.favorite ? 'text-held' : 'text-muted'"
            :aria-pressed="c.favorite"
            :aria-label="`Favorite: ${c.name}`"
            :title="c.favorite ? 'Remove from favorites' : 'Add to favorites'"
            @click="toggleFavorite(c)"
          >
            <Icon name="star" :filled="c.favorite" />
          </button>
          <button
            class="row border-b-0 pl-1"
            :aria-current="selected === c.id"
            :aria-expanded="selected === c.id"
            @click="selected = selected === c.id ? undefined : c.id"
          >
            <!-- Contact fields may come from an import: interpolation only. -->
            <span class="min-w-0 flex-1 truncate font-semibold">{{ c.name }}</span>
            <span class="num truncate text-muted">{{ c.numbers[0]?.number }}</span>
          </button>
        </div>
        <div v-if="selected === c.id" class="flex flex-col gap-1 border-b border-line-subtle p-1">
          <button
            v-for="n in c.numbers"
            :key="n.number"
            class="btn justify-start"
            :disabled="!!ui.call"
            @click="ui.dispatch({ type: 'dialNumber', number: n.number })"
          >
            <Icon name="phone" />Call <span class="num">{{ n.number }}</span>
            <span v-if="n.label" class="text-muted">({{ n.label }})</span>
          </button>
          <div class="grid grid-cols-2 gap-1">
            <button class="btn" @click="ui.dispatch({ type: 'editContact', contact: c })">
              <Icon name="edit" />Edit
            </button>
            <button class="btn" @click="ui.dispatch({ type: 'deleteContact', id: c.id })">
              <Icon name="trash" />Delete
            </button>
          </div>
        </div>
      </li>
      <li v-if="!list.length" class="p-3 text-muted">
        {{ search ? "No contacts match." : "No contacts yet." }}
      </li>
    </ul>
    <div class="flex gap-1">
      <input
        v-model="search"
        class="field"
        type="search"
        placeholder="Search contacts"
        aria-label="Search contacts"
      />
      <button class="btn" @click="ui.dispatch({ type: 'newContact' })">
        <Icon name="plus" />Add
      </button>
    </div>
  </section>
</template>
