<script setup lang="ts">
/**
 * Inline status banner: cache notice, retryable errors, info (docs/03 §11).
 * Business errors are shown inline, never as blocking OS modals.
 */
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

withDefaults(
  defineProps<{
    variant?: 'info' | 'cache' | 'error' | 'success'
    title?: string
    message?: string
    showRetry?: boolean
    retryLabel?: string
  }>(),
  { variant: 'info', title: undefined, message: undefined, showRetry: false, retryLabel: undefined },
)

defineEmits<{ retry: [] }>()
</script>

<template>
  <div class="state-banner" :class="`state-banner-${variant}`" role="status">
    <div class="banner-body">
      <p v-if="title" class="banner-title">{{ title }}</p>
      <p v-if="message" class="banner-message">{{ message }}</p>
      <slot />
    </div>
    <button v-if="showRetry" type="button" class="btn btn-ghost banner-retry" @click="$emit('retry')">
      {{ retryLabel ?? t('common.retry') }}
    </button>
  </div>
</template>

<style scoped>
.state-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-md);
  font-size: 13px;
}

.banner-title {
  font-weight: 600;
}

.banner-message {
  color: var(--text-secondary);
  margin-top: 2px;
  word-break: break-word;
}

.state-banner-info {
  background: var(--bg-inset);
  color: var(--text-primary);
}

.state-banner-cache {
  background: var(--accent-soft);
  color: var(--accent);
}

.state-banner-cache .banner-message {
  color: var(--accent);
  opacity: 0.8;
}

.state-banner-error {
  background: var(--danger-soft);
  color: var(--danger);
}

.state-banner-error .banner-message {
  color: var(--danger);
  opacity: 0.85;
}

.state-banner-success {
  background: var(--success-soft);
  color: var(--success);
}

.banner-retry {
  flex: none;
}
</style>
