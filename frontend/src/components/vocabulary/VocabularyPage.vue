<script setup lang="ts">
/**
 * Vocabulary page: search, cards sorted by saved time desc, empty/loading/
 * error states. Delete is confirmed per card.
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VocabularyItem } from '@/api/types'
import { useVocabularyStore } from '@/stores/vocabulary'
import VocabularyCard from './VocabularyCard.vue'
import IconSearch from '@/components/icons/IconSearch.vue'
import IconBook from '@/components/icons/IconBook.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import StateBanner from '@/components/common/StateBanner.vue'
import ConfirmModal from '@/components/common/ConfirmModal.vue'

const { t } = useI18n()
const vocabulary = useVocabularyStore()

const deleteTarget = ref<VocabularyItem | null>(null)
let searchTimer: ReturnType<typeof setTimeout> | null = null

onMounted(() => {
  void vocabulary.load()
})

function onSearchInput(): void {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    void vocabulary.load(vocabulary.query)
  }, 250)
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return
  await vocabulary.remove(deleteTarget.value.lemma)
  deleteTarget.value = null
}
</script>

<template>
  <section class="vocabulary-page">
    <header class="page-header">
      <h1 class="page-title">{{ t('vocab.title') }}</h1>
      <div class="search-box">
        <IconSearch :size="14" class="search-icon" />
        <input
          v-model="vocabulary.query"
          type="search"
          class="search-input"
          :placeholder="t('vocab.searchPlaceholder')"
          :aria-label="t('vocab.searchPlaceholder')"
          @input="onSearchInput"
        />
      </div>
    </header>

    <LoadingState v-if="vocabulary.status === 'loading'" />
    <StateBanner
      v-else-if="vocabulary.status === 'error'"
      variant="error"
      :title="t('errors.unknown')"
      :message="vocabulary.error ?? ''"
      show-retry
      @retry="vocabulary.load()"
    />
    <EmptyState
      v-else-if="vocabulary.status === 'empty' || vocabulary.items.length === 0"
      :title="t('vocab.emptyTitle')"
      :description="t('vocab.emptyDesc')"
    >
      <template #icon>
        <IconBook :size="32" />
      </template>
    </EmptyState>
    <div v-else class="card-list">
      <VocabularyCard
        v-for="item in vocabulary.items"
        :key="item.lemma"
        :item="item"
        @delete="deleteTarget = $event"
      />
    </div>

    <ConfirmModal
      :open="deleteTarget !== null"
      :title="t('vocab.removeTitle')"
      :message="t('vocab.removeMessage', { lemma: deleteTarget?.lemma ?? '' })"
      :confirm-label="t('common.delete')"
      @confirm="confirmDelete"
      @cancel="deleteTarget = null"
    />
  </section>
</template>

<style scoped>
.vocabulary-page {
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-height: 100%;
}

.page-header {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.page-title {
  font-size: 20px;
  font-weight: 700;
}

.search-box {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-3);
  border: 1px solid var(--hairline-strong);
  border-radius: var(--radius-md);
  background: var(--bg-surface);
  color: var(--text-tertiary);
}

.search-box:focus-within {
  border-color: var(--accent);
}

.search-input {
  flex: 1;
  border: none;
  background: none;
  padding: 8px 0;
  min-width: 0;
}

.search-input:focus {
  outline: none;
}

.card-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
</style>
