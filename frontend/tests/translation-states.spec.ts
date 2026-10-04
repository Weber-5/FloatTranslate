import { describe, expect, it } from 'vitest'
import { setupTestEnv } from './helpers'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'

describe('translation per-tab state machine', () => {
  it('goes empty → input_ready → translating → success on first query', async () => {
    await setupTestEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()
    const tab = tabs.newTab()

    expect(translation.stateFor(tab.id).status).toBe('empty')
    translation.setInput(tab.id, 'hello world')
    expect(translation.stateFor(tab.id).status).toBe('input_ready')

    const pending = translation.translate(tab.id)
    expect(translation.stateFor(tab.id).status).toBe('translating')
    await pending

    const state = translation.stateFor(tab.id)
    expect(state.status).toBe('success')
    expect(state.response?.source).toBe('model')
    expect(state.response?.kind).toBe('text')
  })

  it("shows the 'suspended' frozen contract entry for word queries", async () => {
    await setupTestEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()
    const tab = tabs.newTab()

    translation.setInput(tab.id, 'suspended')
    await translation.translate(tab.id)

    const response = translation.stateFor(tab.id).response
    expect(response?.kind).toBe('word')
    expect(response?.result).toMatchObject({
      word: 'suspended',
      lemma: 'suspend',
      phonetic_uk: 'səˈspendɪd',
      phonetic_us: 'səˈspendɪd',
      parts_of_speech: [
        { part: 'verb', meanings: ['暂停；中止', '悬挂；悬浮'] },
        { part: 'adjective', meanings: ['暂停的；中止的'] },
      ],
      synonyms: ['paused', 'halted', 'deferred'],
      inflections: ['suspend', 'suspends', 'suspending', 'suspended'],
    })
    expect(tabs.activeTab?.title).toBe('suspended')
  })

  it('marks a repeat query as cache_success (source=cache)', async () => {
    await setupTestEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const first = tabs.newTab()
    translation.setInput(first.id, 'the quick brown fox')
    await translation.translate(first.id)
    expect(translation.stateFor(first.id).status).toBe('success')

    const second = tabs.newTab()
    translation.setInput(second.id, 'the quick brown fox')
    await translation.translate(second.id)

    const state = translation.stateFor(second.id)
    expect(state.status).toBe('cache_success')
    expect(state.response?.source).toBe('cache')
  })

  it('enters retryable_error on failure and stays retryable', async () => {
    await setupTestEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'please trigger-error now')
    await translation.translate(tab.id)

    const state = translation.stateFor(tab.id)
    expect(state.status).toBe('retryable_error')
    expect(state.error?.retryable).toBe(true)
    expect(state.response).toBeNull()

    await translation.translate(tab.id)
    expect(translation.stateFor(tab.id).status).toBe('retryable_error')
  })

  it('rejects non-English input with UNSUPPORTED_LANGUAGE', async () => {
    await setupTestEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, '你好，世界')
    await translation.translate(tab.id)

    const state = translation.stateFor(tab.id)
    expect(state.status).toBe('retryable_error')
    expect(state.error?.code).toBe('UNSUPPORTED_LANGUAGE')
    expect(state.error?.retryable).toBe(false)
  })

  it('retranslate bypasses the cache and replaces the result', async () => {
    await setupTestEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'suspend')
    await translation.translate(tab.id)
    const firstId = translation.stateFor(tab.id).response?.translation_id

    await translation.retranslate(tab.id)
    const state = translation.stateFor(tab.id)
    expect(state.status).toBe('success')
    expect(state.response?.source).toBe('model')
    expect(state.response?.translation_id).not.toBe(firstId)
  })
})
