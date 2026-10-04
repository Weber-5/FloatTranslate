<script setup lang="ts">
/**
 * Vocabulary card (docs/03 §7): word, IPA summary, primary meaning, saved
 * date. Click opens a word tab reusing the stored structured content (no LLM
 * call); delete asks for confirmation.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VocabularyItem } from '@/api/types'
import { useTranslationStore } from '@/stores/translation'
import IconButton from '@/components/common/IconButton.vue'
import IconTrash from '@/components/icons/IconTrash.vue'

const props = defineProps<{ item: VocabularyItem }>()

const emit = defineEmits<{ delete: [item: VocabularyItem] }>()

const { t } = useI18n()
const translationStore = useTranslationStore()

const primaryMeaning = computed(() => {
  const group = props.item.word.parts_of_speech[0]
  return group ? group.meanings[0] : ''
})

const ipaSummary = computed(() => {
  const { phonetic_uk, phonetic_us } = props.item.word
  if (phonetic_uk && phonetic_us && phonetic_uk !== phonetic_us) {
    return `UK ${phonetic_uk} · US ${phonetic_us}`
  }
  return phonetic_uk || phonetic_us
})

function open(): void {
  translationStore.openSavedWord(props.item)
}

function formatSavedAt(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
}
</script>

<template>
  <article class="vocab-card card">
    <button type="button" class="card-body" :aria-label="`${t('vocab.openCard')}：${item.lemma}`" @click="open">
      <div class="card-top">
        <span class="word">{{ item.word.word }}</span>
        <span v-if="ipaSummary" class="ipa">{{ ipaSummary }}</span>
      </div>
      <p class="meaning">{{ primaryMeaning }}</p>
      <p class="saved-at">{{ t('vocab.savedAt') }} {{ formatSavedAt(item.saved_at) }}</p>
    </button>
    <IconButton
      :label="`${t('vocab.deleteCard')}：${item.lemma}`"
      tone="danger"
      size="sm"
      class="card-delete"
      @click="emit('delete', item)"
    >
      <IconTrash :size="14" />
    </IconButton>
  </article>
</template>

<style scoped>
.vocab-card {
  position: relative;
  transition:
    box-shadow var(--transition-fast),
    transform var(--transition-fast);
}

.vocab-card:hover {
  box-shadow: var(--shadow-2);
}

.card-body {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: var(--space-4);
  text-align: left;
  border-radius: var(--radius-lg);
}

.card-top {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.word {
  font-size: 16px;
  font-weight: 600;
}

.ipa {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-tertiary);
}

.meaning {
  font-size: 13px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.saved-at {
  font-size: 11px;
  color: var(--text-tertiary);
}

.card-delete {
  position: absolute;
  top: 10px;
  right: 10px;
  opacity: 0;
  transition: opacity var(--transition-fast);
}

.vocab-card:hover .card-delete,
.vocab-card:focus-within .card-delete {
  opacity: 1;
}
</style>
