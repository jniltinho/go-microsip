<script setup lang="ts">
// G0 spike page: proves the embedded page loads, calls Go and reports readiness. The softphone
// screens come with milestone M2.
import { onMounted, ref } from "vue";
import { ready, version } from "./api";

const about = ref("");
const failure = ref("");

onMounted(async () => {
  try {
    about.value = await version();
    await ready();
  } catch (e) {
    failure.value = String(e);
  }
});
</script>

<template>
  <main class="flex h-full flex-col items-center justify-center gap-2 bg-neutral-100 text-neutral-900">
    <h1 class="text-lg font-semibold">go-microsip</h1>
    <p data-testid="version" class="text-sm">{{ about || "…" }}</p>
    <p v-if="failure" role="alert" class="text-sm text-red-700">{{ failure }}</p>
  </main>
</template>
