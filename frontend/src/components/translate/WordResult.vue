<script setup lang="ts">
/**
 * Word structured result (docs/03 §4): big word + favorite star, UK/US IPA
 * with speaker buttons, weak inflections, POS groups, clickable synonym
 * chips, status bar (model / cache / retranslate / Ask AI). No examples.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TranslationResponse, WordTranslation } from '@/api/types'
import { useVocabularyStore } from '@/stores/vocabulary'
import { useTranslationStore } from '@/stores/translation'
import { speak } from '@/services/tts'
import IconButton from '@/components/common/IconButton.vue'
import IconSpeaker from '@/components/icons/IconSpeaker.vue'
import IconStar from '@/components/icons/IconStar.vue'
import IconStarFilled from '@/components/icons/IconStarFilled.vue'
import IconRefresh from '@/components/icons/IconRefresh.vue'
import AskAiButton from './AskAiButton.vue'

const props = defineProps<{ response: TranslationResponse }>()

const { t } = useI18n()
const vocabulary = useVocabularyStore()
const translationStore = useTranslationStore()

const result = computed(() => props.response.result as WordTranslation)
const saved = computed(() => vocabulary.isSaved(result.value.lemma))
const retranslating = ref(false)

const KNOWN_POS = [
  'noun',
  'verb',
  'adjective',
  'adverb',
  'preposition',
  'conjunction',
  'pronoun',
  'interjection',
  'article',
  'other',
]

function posLabel(part: string): string {
  const key = part.toLowerCase()
  return KNOWN_POS.includes(key) ? t(`word.pos.${key}`) : part
}

async function toggleSave(): Promise<void> {
  await vocabulary.toggleSave(result.value)
}

async function onRetranslate(): Promise<void> {
  if (retranslating.value) return
  retranslating.value = true
  try {
    await translationStore.retranslate(props.response.translation_id)
  } finally {
    retranslating.value = false
  }
}

function onSynonym(synonym: string): void {
  translationStore.openWordLookup(synonym)
}

function askText(): string {
  const meanings = result.value.parts_of_speech
    .map((group) => `${group.part}: ${group.meanings.join('；')}`)
    .join('\n')
  return `${result.value.word}\n${meanings}`
}
</script>

<template>
  <article class="word-result card">
    <header class="word-header">
      <h1 class="word">{{ result.word }}</h1>
      <IconButton
        :label="saved ? t('word.removeFromVocabulary') : t('word.addToVocabulary')"
        :tone="saved ? 'accent' : 'default'"
        :aria-pressed="saved"
        @click="toggleSave"
      >
        <IconStarFilled v-if="saved" :size="18" />
        <IconStar v-else :size="18" />
      </IconButton>
    </header>

    <div class="ipa-row">
      <span class="ipa-block">
        <span class="ipa-accent">UK</span>
        <span class="ipa">{{ result.phonetic_uk || '—' }}</span>
        <IconButton :label="t('word.speakUk')" size="sm" @click="speak(result.word, 'en-GB')">
          <IconSpeaker :size="14" />
        </IconButton>
      </span>
      <span class="ipa-block">
        <span class="ipa-accent">US</span>
        <span class="ipa">{{ result.phonetic_us || '—' }}</span>
        <IconButton :label="t('word.speakUs')" size="sm" @click="speak(result.word, 'en-US')">
          <IconSpeaker :size="14" />
        </IconButton>
      </span>
    </div>

    <p v-if="result.inflections.length > 0" class="inflections">
      <span class="inflections-label">{{ t('word.inflections') }}</span>
      <span>{{ result.inflections.join(' · ') }}</span>
    </p>

    <section v-for="group in result.parts_of_speech" :key="group.part" class="pos-group">
      <span class="badge pos-badge">{{ posLabel(group.part) }}</span>
      <ul class="meanings">
        <li v-for="meaning in group.meanings" :key="meaning">{{ meaning }}</li>
      </ul>
    </section>

    <section v-if="result.synonyms.length > 0" class="synonyms">
      <span class="section-title">{{ t('word.synonyms') }}</span>
      <div class="synonym-chips">
        <button
          v-for="synonym in result.synonyms"
          :key="synonym"
          type="button"
          class="chip"
          :aria-label="`${t('translate.emptyTitle')}：${synonym}`"
          data-testid="synonym-chip"
          @click="onSynonym(synonym)"
        >
          {{ synonym }}
        </button>
      </div>
    </section>

    <footer class="status-bar hairline-top">
      <span v-if="response.model" class="model-name">{{ response.model }}</span>
      <span v-if="response.source === 'cache'" class="badge badge-warning">
        {{ t('history.sourceCache') }}
      </span>
      <span class="status-spacer" />
      <button
        type="button"
        class="btn btn-ghost retranslate"
        :disabled="retranslating"
        data-testid="retranslate"
        @click="onRetranslate"
      >
        <IconRefresh :size="14" />
        {{ retranslating ? t('translate.retranslating') : t('translate.retranslate') }}
      </button>
      <AskAiButton :text="askText()" />
    </footer>
  </article>
</template>

<style scoped>
.word-result {
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.word-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.word {
  font-size: 28px;
  font-weight: 700;
  line-height: 1.2;
  word-break: break-word;
}

.ipa-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
}

.ipa-block {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.ipa-accent {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
}

.ipa {
  font-family: var(--font-mono);
  font-size: 13px;
  color: var(--text-secondary);
}

.inflections {
  display: flex;
  gap: var(--space-2);
  font-size: 12px;
  color: var(--text-tertiary);
}

.inflections-label {
  flex: none;
}

.pos-group {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.pos-badge {
  align-self: flex-start;
}

.meanings {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-left: 2px;
}

.meanings li {
  font-size: 14px;
  line-height: 1.6;
}

.synonyms {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.synonym-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.status-bar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding-top: var(--space-3);
  margin-top: var(--space-1);
  font-size: 12px;
  color: var(--text-tertiary);
  flex-wrap: wrap;
}

.model-name {
  font-family: var(--font-mono);
}

.status-spacer {
  flex: 1;
}

.retranslate {
  font-size: 12px;
}
</style>
