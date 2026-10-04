<script setup lang="ts">
/**
 * Text result (docs/03 §5): per paragraph — English source above, Chinese
 * translation below, generous whitespace between pairs. English tokens are
 * clickable (hover/focus affordance only); protected spans (URLs, `code`,
 * numbers, punctuation) keep their plain layout. Click lemmatizes
 * client-side, then opens a new word tab.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TextTranslation, TranslationResponse } from '@/api/types'
import { useTranslationStore } from '@/stores/translation'
import { tokenizeParagraph } from '@/lib/tokenize'
import { lemmatize } from '@/lib/lemmatize'

const props = defineProps<{ response: TranslationResponse }>()

const { t } = useI18n()
const translationStore = useTranslationStore()

const segments = computed(() => (props.response.result as TextTranslation).segments)

function onTokenClick(tokenText: string): void {
  const lemma = lemmatize(tokenText)
  translationStore.openWordLookup(lemma)
}
</script>

<template>
  <section class="text-result">
    <p class="click-hint">{{ t('text.clickHint') }}</p>
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
    </div>
  </section>
</template>

<style scoped>
.text-result {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-5);
}

.click-hint {
  font-size: 12px;
  color: var(--text-tertiary);
  margin-bottom: calc(-1 * var(--space-2));
}

.text-pair {
  padding: var(--space-4) var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.pair-source {
  font-size: 14px;
  line-height: 1.8;
  color: var(--text-primary);
}

.pair-translation {
  font-size: 14px;
  line-height: 1.8;
  color: var(--text-secondary);
  padding-top: var(--space-3);
  border-top: 1px solid var(--hairline);
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
