import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { setupFreshEnv } from './helpers'
import { getClient } from '@/api'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'

describe('tabs persistence (debounced putTabs + restore with hydration)', () => {
  beforeEach(async () => {
    await setupFreshEnv()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('coalesces rapid mutations into ONE debounced putTabs (500ms)', async () => {
    vi.useFakeTimers()
    const tabs = useTabsStore()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'putTabs')

    await tabs.restore()
    expect(putSpy).not.toHaveBeenCalled()

    tabs.openWordTab('alpha')
    tabs.openWordTab('beta')
    tabs.openWordTab('gamma')

    // Not yet flushed.
    await vi.advanceTimersByTimeAsync(100)
    expect(putSpy).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(500)
    expect(putSpy).toHaveBeenCalledTimes(1)
    const persisted = putSpy.mock.calls[0][0] as { id: string }[]
    expect(persisted.length).toBe(4) // restore seed + 3 opened tabs
    expect(persisted[3].id).toBe(tabs.tabs[3].id)
  })

  it('restoreWithHydration refills translation tabs from getTranslation (no provider call)', async () => {
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    const api = await getClient()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'hello world')
    await translation.translate(tab.id)
    const translationId = translation.stateFor(tab.id).response?.translation_id
    expect(translationId).toBeTruthy()
    await tabs.persistNow()

    const createSpy = vi.spyOn(api, 'createTranslation')
    const getSpy = vi.spyOn(api, 'getTranslation')

    // Simulate a restart: drop all local view state, then boot-restore.
    tabs.tabs = []
    tabs.activeId = null
    translation.states = {}
    await translation.restoreTabsWithHydration()

    // newTab() focused the seeded empty tab, so exactly one tab persisted.
    expect(tabs.tabs.length).toBe(1)
    const restored = tabs.tabs[0]
    expect(restored.id).toBe(tab.id)
    expect(restored.payload.translation_id).toBe(translationId)
    // Hydrated from the stored record — createTranslation is never re-run.
    expect(getSpy).toHaveBeenCalledWith(translationId)
    expect(createSpy).not.toHaveBeenCalled()
    const state = translation.stateFor(tab.id)
    expect(state.response).not.toBeNull()
    expect(state.status === 'success' || state.status === 'cache_success').toBe(true)
    expect(state.input).toContain('hello world')
  })

  it('an empty restored tab stays in the empty input state', async () => {
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    // Persist exactly one empty tab.
    await tabs.persistNow()

    tabs.tabs = []
    tabs.activeId = null
    translation.states = {}
    await translation.restoreTabsWithHydration()

    expect(tabs.tabs.length).toBe(1)
    const state = translation.stateFor(tabs.tabs[0].id)
    expect(state.response).toBeNull()
    expect(state.status).toBe('empty')
  })
})
