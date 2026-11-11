<script setup lang="ts">
import { computed, ref } from "vue";
import { saveContact } from "../api";
import { parseDialTarget, validationText } from "../state";
import { ui } from "../store";
import Icon from "./Icon.vue";

const form = computed(() => ui.contactForm);
const error = ref("");

async function save() {
  const c = form.value;
  if (!c) return;
  const name = c.name.trim();
  const numbers = c.numbers
    .map((n) => ({ number: n.number.trim(), label: n.label.trim() }))
    .filter((n) => n.number);
  const bad = numbers.map((n) => parseDialTarget(n.number)).find((p) => !p.ok);
  if (!name) error.value = "Name is required";
  else if (name.length > 64) error.value = "Name is too long";
  else if (!numbers.length) error.value = "At least one number is required";
  else if (bad && !bad.ok) error.value = `Number: ${validationText(bad.error)}`;
  else if (new Set(numbers.map((n) => n.number)).size < numbers.length)
    error.value = "A number is repeated";
  else {
    try {
      await saveContact({ ...c, name, numbers });
      ui.contactsRevision++;
      await ui.dispatch({ type: "selectTab", tab: "contacts" });
    } catch (e) {
      error.value = String(e);
    }
  }
}
</script>

<template>
  <form
    v-if="form"
    class="flex h-full flex-col"
    :aria-label="form.id ? 'Edit contact' : 'New contact'"
    @submit.prevent="save"
  >
    <div class="titlebar">
      {{ form.id ? "Edit contact" : "New contact" }}
      <button
        class="btn"
        type="button"
        aria-label="Close"
        @click="ui.dispatch({ type: 'closeView' })"
      >
        <Icon name="close" />
      </button>
    </div>
    <div class="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-2">
      <label>
        <span class="label">Name</span>
        <input v-model="form.name" class="field" maxlength="64" autocomplete="off" autofocus />
      </label>
      <fieldset class="group flex flex-col gap-1">
        <legend>Numbers</legend>
        <div v-for="(n, i) in form.numbers" :key="i" class="flex gap-1">
          <input
            v-model="n.number"
            class="field num flex-[2]"
            maxlength="64"
            placeholder="Number"
            :aria-label="`Number ${i + 1}`"
            autocomplete="off"
            spellcheck="false"
          />
          <input
            v-model="n.label"
            class="field flex-1"
            maxlength="32"
            placeholder="Label"
            :aria-label="`Label ${i + 1}`"
          />
          <button
            class="btn"
            type="button"
            :disabled="form.numbers.length < 2"
            :aria-label="`Remove number ${i + 1}`"
            @click="form.numbers.splice(i, 1)"
          >
            <Icon name="trash" />
          </button>
        </div>
        <button
          class="btn self-start"
          type="button"
          @click="form.numbers.push({ number: '', label: '' })"
        >
          <Icon name="plus" />Add number
        </button>
      </fieldset>
      <label class="flex items-center gap-1.5"
        ><input v-model="form.favorite" type="checkbox" />Favorite</label
      >
      <p v-if="error" class="flex items-center gap-1 text-error" role="alert">
        <Icon name="alert" />{{ error }}
      </p>
    </div>
    <div class="flex justify-end gap-1 border-t border-line p-2">
      <button class="btn" type="button" @click="ui.dispatch({ type: 'closeView' })">Cancel</button>
      <button class="btn" type="submit"><Icon name="check" />Save</button>
    </div>
  </form>
</template>
