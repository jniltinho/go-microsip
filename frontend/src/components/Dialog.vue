<script setup lang="ts">
// A modal dialog with the skin's title bar, or a sheet at the bottom edge without one. The native
// <dialog> traps focus, makes the rest of the window inert and reports Escape as "cancel"; the
// element marked `autofocus` gets the focus.
import { onMounted, ref } from "vue";

defineProps<{ title: string; sheet?: boolean }>();
const emit = defineEmits<{ cancel: [] }>();
const el = ref<HTMLDialogElement>();
onMounted(() => el.value?.showModal());
</script>

<template>
  <dialog
    ref="el"
    class="dialog"
    :class="{ sheet }"
    :aria-label="title"
    @cancel.prevent="emit('cancel')"
  >
    <div v-if="!sheet" class="titlebar">{{ title }}</div>
    <div class="flex flex-col gap-2" :class="sheet ? 'p-4' : 'p-3'"><slot /></div>
  </dialog>
</template>
