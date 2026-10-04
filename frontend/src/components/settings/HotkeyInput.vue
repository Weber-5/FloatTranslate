<script setup lang="ts">
/**
 * Hotkey recorder (docs/00 §3: 快捷键均可修改，修改后立即重新注册并持久化).
 * Click/focus starts recording: keydown events build a canonical combo string
 * ("Ctrl+Alt+Q" style, modifiers Ctrl/Alt/Shift/Win + key). Esc cancels,
 * Backspace/Delete clears the draft. A combo needs at least one modifier —
 * plain keys show an inline hint and are not committed.
 *
 * While not recording the field always displays `modelValue`, so a rejected
 * save in the parent automatically reverts the visible value.
 */
import { computed, ref, useAttrs } from 'vue'
import { useI18n } from 'vue-i18n'

defineOptions({ inheritAttrs: false })

const props = defineProps<{
  modelValue: string
  label: string
}>()

const attrs = useAttrs()

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const { t } = useI18n()

const boxEl = ref<HTMLElement | null>(null)
const recording = ref(false)
const draft = ref('')
const invalid = ref(false)

const display = computed(() => (recording.value ? draft.value : props.modelValue))
const showPlaceholder = computed(() => display.value.length === 0)

function normalizeKey(event: KeyboardEvent): string | null {
  const key = event.key
  if (/^[a-z]$/i.test(key)) return key.toUpperCase()
  if (/^[0-9]$/.test(key)) return key
  if (/^F([1-9]|1[0-2])$/.test(key)) return key
  if (key === ' ' || key === 'Spacebar') return 'Space'
  if (key === 'Enter') return 'Enter'
  if (key === 'ArrowUp') return 'Up'
  if (key === 'ArrowDown') return 'Down'
  if (key === 'ArrowLeft') return 'Left'
  if (key === 'ArrowRight') return 'Right'
  // Punctuation and other single-character keys keep their literal glyph.
  if (key.length === 1) return key
  return null
}

function comboFrom(event: KeyboardEvent): string | null {
  const key = normalizeKey(event)
  if (key === null) return null
  const parts: string[] = []
  if (event.ctrlKey) parts.push('Ctrl')
  if (event.altKey) parts.push('Alt')
  if (event.shiftKey) parts.push('Shift')
  if (event.metaKey) parts.push('Win')
  parts.push(key)
  return parts.join('+')
}

function startRecording(): void {
  recording.value = true
  draft.value = ''
  invalid.value = false
}

function stopRecording(): void {
  recording.value = false
  draft.value = ''
  invalid.value = false
}

/** Esc: discard the draft and leave recording (value stays unchanged). */
function cancelRecording(): void {
  stopRecording()
  boxEl.value?.blur()
}

function onBlur(): void {
  // Nothing is committed on blur — the draft is simply discarded.
  stopRecording()
}

function onKeyDown(event: KeyboardEvent): void {
  if (!recording.value) return // ignore keys when not recording (e.g. after Esc)
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    cancelRecording()
    return
  }
  if (event.key === 'Tab') return // keep keyboard traversal intact

  const bare = !event.ctrlKey && !event.altKey && !event.metaKey
  if ((event.key === 'Backspace' || event.key === 'Delete') && bare) {
    event.preventDefault()
    draft.value = ''
    invalid.value = false
    return
  }

  const combo = comboFrom(event)
  if (combo === null) return
  event.preventDefault()

  if (!event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey) {
    // Client-side minimal validation: ≥1 modifier required.
    invalid.value = true
    draft.value = ''
    return
  }

  invalid.value = false
  draft.value = combo
  emit('update:modelValue', combo)
  stopRecording()
}
</script>

<template>
  <div class="hotkey-field">
    <button
      ref="boxEl"
      type="button"
      class="hotkey-input"
      :class="{ recording, invalid }"
      :aria-label="label"
      :title="label"
      data-testid="hotkey-input"
      v-bind="attrs"
      @focus="startRecording"
      @blur="onBlur"
      @keydown="onKeyDown"
    >
      <kbd v-if="!showPlaceholder" class="hotkey-value">{{ display }}</kbd>
      <span v-else class="hotkey-placeholder">{{ recording ? t('settings.general.hotkeyRecording') : t('settings.general.hotkeyEmpty') }}</span>
    </button>
    <p v-if="invalid" class="hotkey-invalid" role="alert">
      {{ t('settings.general.hotkeyNeedModifier') }}
    </p>
  </div>
</template>

<style scoped>
.hotkey-field {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.hotkey-input {
  min-width: 132px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px 10px;
  border-radius: var(--radius-md);
  border: 1px solid var(--hairline);
  background: var(--bg-inset);
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--text-primary);
  cursor: pointer;
  transition:
    border-color var(--transition-fast),
    background var(--transition-fast);
}

.hotkey-input:hover {
  border-color: var(--hairline-strong);
}

.hotkey-input.recording {
  border-color: var(--accent);
  background: var(--bg-surface);
}

.hotkey-input.invalid {
  border-color: var(--danger);
}

.hotkey-value {
  font-family: inherit;
  font-size: inherit;
}

.hotkey-placeholder {
  font-family: var(--font-sans);
  color: var(--text-tertiary);
}

.hotkey-invalid {
  font-size: 11px;
  color: var(--danger);
  margin-top: 4px;
}
</style>
