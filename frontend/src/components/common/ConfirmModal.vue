<script setup lang="ts">
/**
 * Confirmation modal for destructive actions (docs/00 §5: destructive
 * operations must be clearly marked). /clear is exempt by user request.
 */
import { useI18n } from 'vue-i18n'
import IconButton from './IconButton.vue'
import IconClose from '@/components/icons/IconClose.vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    message: string
    confirmLabel?: string
    danger?: boolean
  }>(),
  { confirmLabel: undefined, danger: true },
)

const emit = defineEmits<{ confirm: []; cancel: [] }>()

const { t } = useI18n()

function onKeydown(event: KeyboardEvent): void {
  if (!props.open) return
  if (event.key === 'Escape') emit('cancel')
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="modal-backdrop"
      @keydown="onKeydown"
      @click.self="emit('cancel')"
    >
      <div class="modal-card" role="alertdialog" :aria-label="title" aria-modal="true">
        <div class="modal-header">
          <h2 class="modal-title">{{ title }}</h2>
          <IconButton :label="t('common.close')" size="sm" @click="emit('cancel')">
            <IconClose :size="16" />
          </IconButton>
        </div>
        <p class="modal-message">{{ message }}</p>
        <div class="modal-actions">
          <button type="button" class="btn btn-secondary" @click="emit('cancel')">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn"
            :class="danger ? 'btn-danger' : 'btn-primary'"
            @click="emit('confirm')"
          >
            {{ confirmLabel ?? t('common.confirm') }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 60;
}

.modal-card {
  width: min(320px, calc(100vw - 48px));
  background: var(--bg-surface);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-3);
  padding: var(--space-4) var(--space-5) var(--space-5);
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.modal-title {
  font-size: 15px;
  font-weight: 600;
}

.modal-message {
  margin-top: var(--space-2);
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  margin-top: var(--space-4);
}
</style>
