<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ label?: string }>()

const { t } = useI18n()
</script>

<template>
  <div class="loading-state" role="status" :aria-label="label ?? t('common.loading')">
    <span class="spinner" aria-hidden="true" />
    <p class="loading-label">{{ label ?? t('common.loading') }}</p>
  </div>
</template>

<style scoped>
.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  padding: var(--space-6);
  color: var(--text-secondary);
}

.spinner {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid var(--hairline-strong);
  border-top-color: var(--accent);
  animation: spin 0.8s linear infinite;
}

.loading-label {
  font-size: 13px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
