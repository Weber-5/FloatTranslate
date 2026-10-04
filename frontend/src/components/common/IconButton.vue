<script setup lang="ts">
/**
 * Icon-only button. Always carries an aria-label and a tooltip (docs/03 §12).
 */
withDefaults(
  defineProps<{
    label: string
    size?: 'sm' | 'md'
    tone?: 'default' | 'danger' | 'accent'
    disabled?: boolean
  }>(),
  { size: 'md', tone: 'default', disabled: false },
)

defineEmits<{ click: [event: MouseEvent] }>()
</script>

<template>
  <button
    type="button"
    class="icon-btn"
    :class="[`icon-btn-${size}`, `icon-btn-${tone}`]"
    :aria-label="label"
    :title="label"
    :disabled="disabled"
    @click="$emit('click', $event)"
  >
    <slot />
  </button>
</template>

<style scoped>
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-md);
  color: var(--text-secondary);
  transition:
    background var(--transition-fast),
    color var(--transition-fast);
  flex: none;
}

.icon-btn-sm {
  width: 22px;
  height: 22px;
}

.icon-btn-md {
  width: 30px;
  height: 30px;
}

.icon-btn:hover:not(:disabled),
.icon-btn:focus-visible {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.icon-btn-accent {
  color: var(--accent);
}

.icon-btn-danger:hover:not(:disabled),
.icon-btn-danger:focus-visible {
  background: var(--danger-soft);
  color: var(--danger);
}
</style>
