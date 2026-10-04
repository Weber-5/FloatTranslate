/**
 * Per-tab translation state machine (docs/01 §3):
 *   empty → input_ready → translating → success | cache_success | retryable_error
 * Retranslate re-enters translating from success/cache_success and replaces
 * the current result; on failure the previous result is kept and an inline
 * retryable error is shown.
 */
import { ref } from 'vue'
import { defineStore } from 'pinia'
import type {
  HistoryItem,
  TranslationResponse,
  VocabularyItem,
  WordTranslation,
} from '@/api/types'
import { useApi, toApiError } from '@/api'
import { useTabsStore } from './tabs'

export type TranslationStatus =
  | 'empty'
  | 'input_ready'
  | 'translating'
  | 'success'
  | 'cache_success'
  | 'retryable_error'

export interface TranslationError {
  code: string
  message: string
  retryable: boolean
}

export interface TabTranslationState {
  status: TranslationStatus
  input: string
  response: TranslationResponse | null
  error: TranslationError | null
}

function emptyState(): TabTranslationState {
  return { status: 'empty', input: '', response: null, error: null }
}

function isWordResult(result: TranslationResponse['result']): result is WordTranslation {
  return 'lemma' in result
}

export const useTranslationStore = defineStore('translation', () => {
  const states = ref<Record<string, TabTranslationState>>({})

  function stateFor(tabId: string): TabTranslationState {
    const existing = states.value[tabId]
    if (existing) return existing
    const created = emptyState()
    states.value[tabId] = created
    return created
  }

  function ensureState(tabId: string): TabTranslationState {
    return stateFor(tabId)
  }

  function setInput(tabId: string, text: string): void {
    const state = ensureState(tabId)
    state.input = text
    if (state.status === 'empty' || state.status === 'input_ready') {
      state.status = text.trim().length > 0 ? 'input_ready' : 'empty'
    }
  }

  async function translate(tabId: string, opts?: { bypassCache?: boolean }): Promise<void> {
    const state = ensureState(tabId)
    const text = state.input.trim()
    if (text.length === 0 || state.status === 'translating') return
    state.status = 'translating'
    state.error = null
    try {
      const response = await useApi().createTranslation(text, undefined, opts?.bypassCache ?? false)
      state.response = response
      state.status = response.source === 'cache' ? 'cache_success' : 'success'
      useTabsStore().applyTranslationResult(tabId, response)
    } catch (err) {
      const apiError = toApiError(err)
      state.error = { code: apiError.code, message: apiError.message, retryable: apiError.retryable }
      state.status = 'retryable_error'
    }
  }

  async function retranslate(tabId: string): Promise<void> {
    const state = ensureState(tabId)
    const translationId = state.response?.translation_id
    if (!translationId || state.status === 'translating') return
    state.status = 'translating'
    state.error = null
    try {
      const response = await useApi().retranslate(translationId)
      state.response = response
      state.status = response.source === 'cache' ? 'cache_success' : 'success'
      useTabsStore().applyTranslationResult(tabId, response)
    } catch (err) {
      const apiError = toApiError(err)
      state.error = { code: apiError.code, message: apiError.message, retryable: apiError.retryable }
      // Keep the previous result visible; the view shows the inline error.
      state.status = 'retryable_error'
    }
  }

  /** Click a synonym chip / text token / vocabulary card → new word tab. */
  function openWordLookup(word: string, opts?: { wordData?: WordTranslation }): string {
    const tabsStore = useTabsStore()
    const tab = tabsStore.openWordTab(word, { wordData: opts?.wordData })
    const state = ensureState(tab.id)
    if (opts?.wordData) {
      // Local structured content (e.g. from the vocabulary): no LLM call.
      state.response = {
        translation_id: `vocab:${opts.wordData.lemma}`,
        kind: 'word',
        source: 'model',
        result: opts.wordData,
        model: '',
        created_at: new Date().toISOString(),
      }
      state.status = 'success'
      state.error = null
    } else {
      state.input = word
      state.status = 'input_ready'
      void translate(tab.id)
    }
    return tab.id
  }

  /** Vocabulary card: reuse stored structured content, never re-query the LLM. */
  function openSavedWord(item: VocabularyItem): string {
    return openWordLookup(item.word.word, { wordData: item.word })
  }

  /**
   * US-04 划词即译: the host captured the selected text and woke the window.
   * Opens a NEW text tab carrying the captured text as input and auto-sends
   * the translation (translate() flips the state machine to `translating`
   * synchronously before the first await).
   */
  function openSelectionTab(text: string): string {
    const tabsStore = useTabsStore()
    const tab = tabsStore.openTextTab(text)
    const state = ensureState(tab.id)
    state.input = text
    state.status = 'input_ready'
    state.error = null
    void translate(tab.id)
    return tab.id
  }

  /**
   * History drawer: reopen as a new tab. The result is loaded from the stored
   * record via GET /translations/{id} — a plain DB read, never a provider
   * call. Falls back to the history payload if the record is gone.
   */
  async function openHistoryItem(item: HistoryItem): Promise<string> {
    const tabsStore = useTabsStore()
    const tab = tabsStore.openFromHistory(item)
    const state = ensureState(tab.id)
    state.status = 'translating'
    state.error = null
    try {
      const response = await useApi().getTranslation(item.id)
      hydrateFromResponse(tab.id, response)
    } catch {
      state.response = {
        translation_id: item.id,
        kind: item.kind,
        source: item.source ?? 'model',
        result: item.result,
        model: item.model ?? '',
        created_at: item.created_at,
      }
      state.status = 'success'
      state.error = null
    }
    return tab.id
  }

  /** Seed a tab from a full response (used when restoring persisted tabs). */
  function hydrateFromResponse(tabId: string, response: TranslationResponse): void {
    const state = ensureState(tabId)
    state.response = response
    state.status = response.source === 'cache' ? 'cache_success' : 'success'
    state.error = null
    if (isWordResult(response.result)) state.input = response.result.word
    else state.input = response.result.source_markdown
  }

  /**
   * Boot restore incl. content hydration (docs/00 §2 重启恢复): tabs carrying
   * a translation_id are refilled via GET /translations/{id} — a plain DB
   * read, never a provider call; tabs with stored word_data keep their local
   * structured content; empty tabs stay in the empty input state.
   */
  async function restoreTabsWithHydration(): Promise<void> {
    const tabsStore = useTabsStore()
    try {
      await tabsStore.restore()
    } catch {
      tabsStore.ensureTab()
      return
    }
    await Promise.all(
      tabsStore.tabs.map(async (tab) => {
        const translationId = tab.payload.translation_id
        if (!translationId || tab.payload.word_data || stateFor(tab.id).response) {
          return
        }
        try {
          const response = await useApi().getTranslation(translationId)
          hydrateFromResponse(tab.id, response)
        } catch {
          // Stored translation no longer available — the tab stays an input tab.
        }
      }),
    )
  }

  return {
    states,
    stateFor,
    ensureState,
    setInput,
    translate,
    retranslate,
    openWordLookup,
    openSavedWord,
    openSelectionTab,
    openHistoryItem,
    hydrateFromResponse,
    restoreTabsWithHydration,
  }
})
