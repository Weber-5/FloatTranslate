<script setup lang="ts">
/**
 * Translation input (docs/03 §3): input area first, Enter sends,
 * Shift+Enter newline, paste only fills (never auto-requests), send button
 * always available.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTranslationStore } from '@/stores/translation'
import IconSend from '@/components/icons/IconSend.vue'
import EmptyState from '@/components/common/EmptyState.vue'

const props = defineProps<{ tabId: string }>()

const { t } = useI18n()
const translationStore = useTranslationStore()

const textareaEl = ref<HTMLTextAreaElement | null>(null)

const state = computed(() => translationStore.stateFor(props.tabId))

const draft = computed({
  get: () => state.value.input,
  set: (value: string) => translationStore.setInput(props.tabId, value),
})

const busy = computed(() => state.value.status === 'translating')

onMounted(() => {
  textareaEl.value?.focus()
})

function submit(): void {
  const text = draft.value.trim()
  if (text.length === 0 || busy.value) return
  translationStore.setInput(props.tabId, text)
  void translationStore.translate(props.tabId)
}

function onEnter(event: KeyboardEvent): void {
  if (event.isComposing) return
  event.preventDefault()
  submit()
}
</script>

<template>
  <div class="translate-input">
    <EmptyState :title="t('translate.emptyTitle')" :description="t('translate.emptyDesc')" />
    <div class="input-card card">
      <textarea
        ref="textareaEl"
        v-model="draft"
        class="translate-textarea"
        rows="4"
        :placeholder="t('translate.placeholder')"
        :aria-label="t('translate.placeholder')"
        :disabled="busy"
        @keydown.enter.exact="onEnter"
      />
      <div class="input-footer hairline-top">
        <span class="hint">{{ t('translate.inputHint') }}</span>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="draft.trim().length === 0 || busy"
          data-testid="translate-send"
          @click="submit"
        >
          <IconSend :size="14" />
          {{ t('translate.send') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.translate-input {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-5);
}

.input-card {
  display: flex;
  flex-direction: column;
}

.translate-textarea {
  border: none;
  background: none;
  padding: var(--space-4);
  font-size: 14px;
  line-height: 1.7;
  min-height: 96px;
}

.translate-textarea:focus {
  outline: none;
}

.translate-textarea:disabled {
  opacity: 0.6;
}

.input-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3) var(--space-2) var(--space-4);
}

.hint {
  font-size: 11px;
  color: var(--text-tertiary);
}
</style>
