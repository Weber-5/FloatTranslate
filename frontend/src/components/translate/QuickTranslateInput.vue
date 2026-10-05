<script setup lang="ts">
/**
 * Inline re-translate box (improvement bug #8).
 *
 * Shown ABOVE an existing Word/Text result so the reader can look up the next
 * word without opening a new tab: the text is translated in the CURRENT tab
 * and replaces the visible result. Every request is still recorded in
 * translation history, so earlier lookups stay reachable from 浏览记录.
 *
 * Enter translates; while a request is in flight the field is disabled and the
 * button reports progress. Mock mode is no different — the store handles both.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTranslationStore } from '@/stores/translation'
import IconSend from '@/components/icons/IconSend.vue'

const props = defineProps<{ tabId: string }>()

const { t } = useI18n()
const translationStore = useTranslationStore()

const draft = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

const busy = computed(() => translationStore.stateFor(props.tabId).status === 'translating')

/** Switching tabs must not carry the half-typed query into the next tab. */
watch(
  () => props.tabId,
  () => {
    draft.value = ''
  },
)

function submit(): void {
  const text = draft.value.trim()
  if (text.length === 0 || busy.value) return
  draft.value = ''
  translationStore.setInput(props.tabId, text)
  void translationStore.translate(props.tabId)
  inputEl.value?.blur()
}

function onEnter(event: KeyboardEvent): void {
  if (event.isComposing) return
  event.preventDefault()
  submit()
}
</script>

<template>
  <form class="quick-translate" data-testid="quick-translate" @submit.prevent="submit">
    <input
      ref="inputEl"
      v-model="draft"
      type="text"
      class="quick-input"
      :placeholder="t('translate.quickPlaceholder')"
      :aria-label="t('translate.quickPlaceholder')"
      :title="t('translate.quickHint')"
      :disabled="busy"
      data-testid="quick-translate-input"
      @keydown.enter="onEnter"
    />
    <button
      type="submit"
      class="btn btn-primary quick-send"
      :disabled="draft.trim().length === 0 || busy"
      :aria-label="busy ? t('translate.translating') : t('translate.send')"
      data-testid="quick-translate-send"
    >
      <IconSend :size="14" />
      {{ busy ? t('translate.translating') : t('translate.send') }}
    </button>
  </form>
</template>

<style scoped>
.quick-translate {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-lg);
  background: var(--bg-surface);
}

.quick-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: none;
  padding: var(--space-1) var(--space-2);
  font-size: 13px;
  color: var(--text-primary);
}

.quick-input:focus {
  outline: none;
}

.quick-input:disabled {
  opacity: 0.6;
}

.quick-send {
  flex: none;
  font-size: 12px;
  padding: var(--space-1) var(--space-3);
}
</style>
