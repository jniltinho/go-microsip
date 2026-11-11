<script setup lang="ts">
// A modal dialog with the skin's title bar. The native <dialog> traps focus, makes the rest of the
// window inert and reports Escape as "cancel"; the element marked `autofocus` gets the focus.
import { onMounted, ref } from "vue";

defineProps<{ title: string }>();
const emit = defineEmits<{ cancel: [] }>();
const el = ref<HTMLDialogElement>();
onMounted(() => el.value?.showModal());
</script>

<template>
  <dialog ref="el" class="dialog" :aria-label="title" @cancel.prevent="emit('cancel')">
    <div class="titlebar">
      <span class="titlebar-text">{{ title }}</span>
    </div>
    <div class="flex flex-col gap-2 p-3"><slot /></div>
  </dialog>
</template>
