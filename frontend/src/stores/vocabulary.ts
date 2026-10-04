/**
 * Vocabulary store: list/search/save/delete, sorted by saved time desc,
 * deduplicated by lemma (docs/00 §5).
 */
import { ref } from 'vue'
import { defineStore } from 'pinia'
import type { VocabularyItem, WordTranslation } from '@/api/types'
import { useApi, toApiError } from '@/api'

export type VocabularyStatus = 'idle' | 'loading' | 'success' | 'empty' | 'error'

export const useVocabularyStore = defineStore('vocabulary', () => {
  const items = ref<VocabularyItem[]>([])
  const status = ref<VocabularyStatus>('idle')
  const query = ref('')
  const error = ref<string | null>(null)

  function isSaved(lemma: string): boolean {
    return items.value.some((item) => item.lemma === lemma.toLowerCase())
  }

  async function load(nextQuery?: string): Promise<void> {
    if (nextQuery !== undefined) query.value = nextQuery
    status.value = 'loading'
    error.value = null
    try {
      const result = await useApi().listVocabulary(query.value)
      items.value = result
      status.value = result.length === 0 ? 'empty' : 'success'
    } catch (err) {
      error.value = toApiError(err).message
      status.value = 'error'
    }
  }

  async function toggleSave(word: WordTranslation): Promise<boolean> {
    const api = useApi()
    if (isSaved(word.lemma)) {
      await api.deleteVocabulary(word.lemma)
    } else {
      await api.saveVocabulary(word.lemma, word)
    }
    await load()
    return isSaved(word.lemma)
  }

  async function remove(lemma: string): Promise<void> {
    await useApi().deleteVocabulary(lemma)
    await load()
  }

  return { items, status, query, error, isSaved, load, toggleSave, remove }
})
