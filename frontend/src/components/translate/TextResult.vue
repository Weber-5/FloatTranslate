<script setup lang="ts">
/**
 * Text result (docs/03 §5): per paragraph — English source above, Chinese
 * translation below, generous whitespace between pairs. English tokens are
 * clickable (hover/focus affordance only); protected spans (URLs, `code`,
 * numbers, punctuation) keep their plain layout. Click lemmatizes
 * client-side, then opens a new word tab. Phase 3: per-segment and
 * whole-result copy of the translation; long tokens wrap instead of
 * causing horizontal overflow.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TextTranslation, TranslationResponse } from '@/api/types'
import { useTranslationStore } from '@/stores/translation'
import { copyText } from '@/services/clipboard'
import { tokenizeParagraph } from '@/lib/tokenize'
import { lemmatize } from '@/lib/lemmatize'
import AskAiButton from './AskAiButton.vue'

const props = defineProps<{ response: TranslationResponse }>()

const { t } = useI18n()
const translationStore = useTranslationStore()

const segments = computed(() => (props.response.result as TextTranslation).segments)

/** Ask AI carries the source text (docs/06 §12), same as the word path. */
const askText = computed(() => (props.response.result as TextTranslation).source_markdown)

const copiedIndex = ref<number | null>(null)
const copiedAll = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | null = null

function flash(marker: () => void): void {
  if (copiedTimer) clearTimeout(copiedTimer)
  marker()
  copiedTimer = setTimeout(() => {
    copiedIndex.value = null
    copiedAll.value = false
  }, 1500)
}

async function copySegment(index: number): Promise<void> {
  const ok = await copyText(segments.value[index]?.translation ?? '')
  if (ok) flash(() => (copiedIndex.value = index))
}

async function copyWhole(): Promise<void> {
  const result = props.response.result as TextTranslation
  const ok = await copyText(result.translated_markdown)
  if (ok) flash(() => (copiedAll.value = true))
}

function onTokenClick(tokenText: string): void {
  const lemma = lemmatize(tokenText)
  translationStore.openWordLookup(lemma)
}
</script>

<template>
  <section class="text-result">
    <div class="text-toolbar">
      <p class="click-hint">{{ t('text.clickHint') }}</p>
      <div class="toolbar-actions">
        <button
          type="button"
          class="btn btn-ghost copy-btn"
          data-testid="text-copy-all"
          @click="copyWhole"
        >
          {{ copiedAll ? t('text.copied') : t('text.copyAll') }}
        </button>
        <AskAiButton :text="askText" />
      </div>
    </div>
    <div v-for="(segment, index) in segments" :key="index" class="text-pair card">
      <p class="pair-source" lang="en">
        <template v-for="(token, tokenIndex) in tokenizeParagraph(segment.source)" :key="tokenIndex">
          <button
            v-if="token.kind === 'word'"
            type="button"
            class="token"
            :data-token="token.text"
            @click="onTokenClick(token.text)"
          >
            {{ token.text }}
          </button>
          <span v-else :class="{ protected: token.kind === 'protected' }">{{ token.text }}</span>
        </template>
      </p>
      <p class="pair-translation">{{ segment.translation }}</p>
      <div class="pair-footer">
        <button
          type="button"
          class="btn btn-ghost copy-btn"
          data-testid="segment-copy"
          :aria-label="t('text.copySegment')"
          @click="copySegment(index)"
        >
          {{ copiedIndex === index ? t('text.copied') : t('text.copySegment') }}
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.text-result {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-5);
  min-width: 0;
}

.text-toolbar {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: calc(-1 * var(--space-2));
}

.click-hint {
  font-size: 12px;
  color: var(--text-tertiary);
}

.toolbar-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: none;
}

.copy-btn {
  font-size: 12px;
  color: var(--text-secondary);
  flex: none;
}

.text-pair {
  padding: var(--space-4) var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.pair-source {
  font-size: 14px;
  line-height: 1.8;
  color: var(--text-primary);
  overflow-wrap: anywhere;
  word-break: break-word;
}

.pair-translation {
  font-size: 14px;
  line-height: 1.8;
  color: var(--text-secondary);
  padding-top: var(--space-3);
  border-top: 1px solid var(--hairline);
  overflow-wrap: anywhere;
  word-break: break-word;
}

.pair-footer {
  display: flex;
  justify-content: flex-end;
  margin-top: calc(-1 * var(--space-1));
}

.token {
  display: inline;
  padding: 0 1px;
  border-radius: 4px;
  color: inherit;
  font-size: inherit;
  line-height: inherit;
  transition:
    background var(--transition-fast),
    color var(--transition-fast);
}

/* Hover/focus affordance only — tokens read as normal prose otherwise. */
.token:hover,
.token:focus-visible {
  background: var(--accent-soft);
  color: var(--accent);
}

.protected {
  font-family: var(--font-mono);
  font-size: 13px;
}
</style>
