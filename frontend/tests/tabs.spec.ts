import { describe, expect, it, beforeEach } from 'vitest'
import { setupTestEnv } from './helpers'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import { useApi } from '@/api'

describe('tabs store (create / close / activate / restore)', () => {
  beforeEach(async () => {
    await setupTestEnv()
  })

  it('restores at least one empty tab when nothing is persisted', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    expect(tabs.tabs).toHaveLength(1)
    expect(tabs.activeTab).not.toBeNull()
    expect(tabs.tabs[0].title.length).toBeGreaterThan(0)
  })

  it('newTab focuses the current empty tab instead of stacking empties', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const first = tabs.newTab()
    const second = tabs.newTab()
    expect(second.id).toBe(first.id)
    expect(tabs.tabs).toHaveLength(1)
  })

  it('openWordTab creates and activates a word tab', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    tabs.newTab()
    const tab = tabs.openWordTab('hello')
    expect(tabs.tabs).toHaveLength(2)
    expect(tab.kind).toBe('word')
    expect(tab.title).toBe('hello')
    expect(tabs.activeId).toBe(tab.id)
  })

  it('applyTranslationResult sets kind, title and payload translation id', async () => {
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()
    const tab = tabs.newTab()
    translation.setInput(tab.id, 'hello world')
    await translation.translate(tab.id)
    const updated = tabs.tabs.find((entry) => entry.id === tab.id)
    expect(updated?.kind).toBe('text')
    expect(updated?.payload.translation_id).toBeTruthy()
    expect(updated?.title).toContain('hello')
  })

  it('close removes the tab and activates a neighbor, never leaving zero tabs', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const word = tabs.openWordTab('run')
    expect(tabs.tabs).toHaveLength(2)
    tabs.close(word.id)
    expect(tabs.tabs).toHaveLength(1)
    expect(tabs.activeId).toBe(tabs.tabs[0].id)
    tabs.close(tabs.tabs[0].id)
    expect(tabs.tabs).toHaveLength(1)
  })

  it('moveTab reorders tabs', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const emptyId = tabs.tabs[0].id
    const a = tabs.openWordTab('a')
    const b = tabs.openWordTab('b')
    const c = tabs.openWordTab('c')
    // Move "a" (index 1) to the end.
    tabs.moveTab(1, 3)
    expect(tabs.tabs.map((tab) => tab.id)).toEqual([emptyId, b.id, c.id, a.id])
  })

  it('persists tabs via putTabs and restores them (open + active)', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const wordTab = tabs.openWordTab('suspend')
    const textTab = tabs.openTextTab('hello world\nsecond line')
    tabs.activate(wordTab.id)
    await tabs.persistNow()

    const persisted = await useApi().getTabs()
    expect(persisted.length).toBe(3)
    expect(persisted.find((tab) => tab.id === wordTab.id)?.is_active).toBe(true)
    expect(persisted.find((tab) => tab.id === textTab.id)?.payload.text).toContain('hello')

    // Simulate restart: wipe local view state, then restore from the API.
    tabs.tabs = []
    tabs.activeId = null
    await tabs.restore()
    expect(tabs.tabs).toHaveLength(3)
    expect(tabs.activeId).toBe(wordTab.id)
    const restoredWord = tabs.tabs.find((tab) => tab.id === wordTab.id)
    expect(restoredWord?.kind).toBe('word')
    expect(restoredWord?.payload.word).toBe('suspend')
  })
})
