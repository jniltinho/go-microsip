<script setup lang="ts">
import { ui } from "../store";
import { SKIN_CHOICES } from "../themes";
</script>

<template>
  <div class="grid grid-cols-2 gap-x-3 gap-y-1" role="radiogroup" aria-label="Skin">
    <label v-for="skin in SKIN_CHOICES" :key="skin.id" class="flex items-center gap-1.5">
      <input
        type="radio"
        name="skin"
        :value="skin.id"
        :checked="ui.theme === skin.id"
        @change="ui.dispatch({ type: 'setTheme', theme: skin.id })"
      />
      {{ skin.label }}
    </label>
  </div>
</template>
