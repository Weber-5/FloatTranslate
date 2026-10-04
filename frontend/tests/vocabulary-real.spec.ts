import { describe, expect, it, vi, afterEach } from 'vitest'
import { setupFreshEnv } from './helpers'
import { getClient } from '@/api'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import { useVocabularyStore } from '@/stores/vocabulary'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('vocabulary real wiring (Phase 2 contract)', () => {
  it('toggles the star: saveVocabulary(lemma, result) then deleteVocabulary', async () => {
    await setupFreshEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    const vocabulary = useVocabularyStore()
    await tabs.restore()
    await vocabulary.load()

    const api = await getClient()
    const saveSpy = vi.spyOn(api, 'saveVocabulary')
    const deleteSpy = vi.spyOn(api, 'deleteVocabulary')

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'suspended')
    await translation.translate(tab.id)
    const result = translation.stateFor(tab.id).response?.result
    expect(result).toBeTruthy()
    if (!('lemma' in result!)) throw new Error('expected a word result')

    expect(vocabulary.isSaved(result.lemma)).toBe(false)
    await vocabulary.toggleSave(result)
    expect(saveSpy).toHaveBeenCalledWith(result.lemma, result)
    expect(vocabulary.isSaved(result.lemma)).toBe(true)

    // Toggling again removes the entry.
    await vocabulary.toggleSave(result)
    expect(deleteSpy).toHaveBeenCalledWith(result.lemma)
    expect(vocabulary.isSaved(result.lemma)).toBe(false)
  })

  it('opens a saved card from stored word data — no LLM call', async () => {
    await setupFreshEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    const vocabulary = useVocabularyStore()
    await tabs.restore()
    await vocabulary.load()

    const api = await getClient()
    const createSpy = vi.spyOn(api, 'createTranslation')

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'suspended')
    await translation.translate(tab.id)
    const result = translation.stateFor(tab.id).response?.result
    if (!('lemma' in result!)) throw new Error('expected a word result')
    await vocabulary.toggleSave(result)
    await vocabulary.load()
    const saved = vocabulary.items.find((item) => item.lemma === result.lemma)
    expect(saved).toBeTruthy()

    createSpy.mockClear()
    const newTabId = translation.openSavedWord(saved!)
    const state = translation.stateFor(newTabId)
    expect(state.status).toBe('success')
    expect(state.response?.result).toEqual(saved!.word)
    expect(createSpy).not.toHaveBeenCalled()
    expect(tabs.activeId).toBe(newTabId)
  })

  it('search passes the query param to listVocabulary', async () => {
    await setupFreshEnv()
    const vocabulary = useVocabularyStore()
    const api = await getClient()
    const listSpy = vi.spyOn(api, 'listVocabulary')

    await vocabulary.load('sus')
    expect(listSpy).toHaveBeenCalledWith('sus')
  })
})
