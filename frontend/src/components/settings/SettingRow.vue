<script setup lang="ts">
/** Shared settings row: label on the left, control on the right. */
defineProps<{
  label: string
  hint?: string
}>()
</script>

<template>
  <div class="setting-row">
    <div class="row-text">
      <span class="row-label">{{ label }}</span>
      <span v-if="hint" class="row-hint">{{ hint }}</span>
    </div>
    <div class="row-control">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-2) 0;
}

.row-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.row-label {
  font-size: 13px;
}

.row-hint {
  font-size: 11px;
  color: var(--text-tertiary);
}

.row-control {
  flex: none;
  max-width: 60%;
}
</style>
