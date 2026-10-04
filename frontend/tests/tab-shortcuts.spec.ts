import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import { handleTabShortcut } from '@/composables/useTabShortcuts'
import TabBar from '@/components/layout/TabBar.vue'
import { useTabsStore } from '@/stores/tabs'

function key(keyText: string, modifiers: Partial<KeyboardEventInit> = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', { key: keyText, ...modifiers })
}

beforeEach(async () => {
  await setupTestEnv()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('in-app tab shortcuts (Ctrl+T / Ctrl+W)', () => {
  it('Ctrl+T opens a new tab from a non-empty active tab', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    tabs.openWordTab('run')
    expect(tabs.tabs).toHaveLength(2)

    expect(handleTabShortcut(key('t', { ctrlKey: true }))).toBe(true)
    expect(tabs.tabs).toHaveLength(3)
    expect(tabs.activeTab?.payload.translation_id ?? tabs.activeTab?.payload.text ?? tabs.activeTab?.payload.word).toBeUndefined()
    expect(tabs.activeTab?.kind).toBe('text')
  })

  it('Ctrl+T focuses the current empty tab instead of stacking empties', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    expect(tabs.tabs).toHaveLength(1)

    handleTabShortcut(key('t', { ctrlKey: true }))
    expect(tabs.tabs).toHaveLength(1)
  })

  it('Ctrl+W closes the active tab and activates a neighbor', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const word = tabs.openWordTab('run')
    handleTabShortcut(key('w', { ctrlKey: true }))
    expect(tabs.tabs).toHaveLength(1)
    expect(tabs.tabs.some((tab) => tab.id === word.id)).toBe(false)
  })

  it('closing the last tab keeps a fresh empty text tab alive', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    handleTabShortcut(key('w', { ctrlKey: true }))
    expect(tabs.tabs).toHaveLength(1)
    expect(tabs.activeTab).not.toBeNull()
    expect(tabs.activeTab?.kind).toBe('text')
    expect(tabs.activeTab?.title.length).toBeGreaterThan(0)
  })

  it('ignores plain keys, other keys and extra modifiers', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const before = tabs.tabs.length

    expect(handleTabShortcut(key('t'))).toBe(false)
    expect(handleTabShortcut(key('w'))).toBe(false)
    expect(handleTabShortcut(key('t', { ctrlKey: true, shiftKey: true }))).toBe(false)
    expect(handleTabShortcut(key('t', { ctrlKey: true, altKey: true }))).toBe(false)
    expect(handleTabShortcut(key('t', { metaKey: true }))).toBe(false)
    expect(handleTabShortcut(key('x', { ctrlKey: true }))).toBe(false)
    expect(tabs.tabs.length).toBe(before)
  })
})

describe('tab bar interactions', () => {
  it('middle-click closes a tab, plain click does not', async () => {
    const { i18n } = await setupTestEnv()
    const tabs = useTabsStore()
    await tabs.restore()
    const word = tabs.openWordTab('run')

    const wrapper = mount(TabBar, { global: { plugins: [i18n] } })
    const items = wrapper.findAll('[data-testid="tab-item"]')
    expect(items).toHaveLength(2)

    const wordTab = items.find((item) => item.text().includes('run'))
    if (!wordTab) throw new Error('word tab not found')

    await wordTab.trigger('auxclick', { button: 0 })
    expect(tabs.tabs.some((tab) => tab.id === word.id)).toBe(true)

    await wordTab.trigger('auxclick', { button: 1 })
    expect(tabs.tabs.some((tab) => tab.id === word.id)).toBe(false)
    expect(tabs.tabs).toHaveLength(1)
  })

  it('documents the shortcuts: + button tooltip and tab bar hint', async () => {
    const { i18n } = await setupTestEnv()
    const tabs = useTabsStore()
    await tabs.restore()

    const wrapper = mount(TabBar, { global: { plugins: [i18n] } })
    const bar = wrapper.find('[data-testid="tab-bar"]')
    expect(bar.attributes('title')).toContain('Ctrl+T')
    expect(bar.attributes('title')).toContain('Ctrl+W')

    const newButton = wrapper.find('.tab-new')
    expect(newButton.attributes('aria-label')).toContain('Ctrl+T')
  })

  it('an empty input tab is titled 新建标签页', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    expect(tabs.tabs[0].title).toBe('新建标签页')
  })

  it('text tab titles use the first ~12 chars of the input', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    const tab = tabs.openTextTab('The quick brown fox jumps over the lazy dog')
    expect(tab.title).toBe('The quick br…')
    const short = tabs.openTextTab('Short text')
    expect(short.title).toBe('Short text')
  })
})
