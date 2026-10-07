/**
 * Improvement feedback: opening a tab from another page must JUMP to 翻译.
 *
 * 单词本 卡片 and 浏览记录 entries used to only open the tab behind the current
 * page, so the user had to click 翻译 by hand before seeing anything.
 */
import { describe, expect, it, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '@/router'
import { setupFreshEnv } from './helpers'
import { showTranslatePage } from '@/services/navigation'
import VocabularyCard from '@/components/vocabulary/VocabularyCard.vue'
import HistoryDrawer from '@/components/layout/HistoryDrawer.vue'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import { useUiStore } from '@/stores/ui'
import type { VocabularyItem } from '@/api/types'

function wordItem(): VocabularyItem {
  return {
    lemma: 'run',
    saved_at: '2026-10-07T00:00:00Z',
    last_viewed_at: '2026-10-07T00:00:00Z',
    word: {
      word: 'run',
      lemma: 'run',
      phonetic_uk: '/rʌn/',
      phonetic_us: '/rʌn/',
      parts_of_speech: [{ part: '动词', meanings: ['跑；奔跑'] }],
      synonyms: ['sprint'],
      inflections: ['running', 'ran'],
    },
  }
}

async function goTo(name: string): Promise<void> {
  await router.push({ name })
  await router.isReady()
  expect(router.currentRoute.value.name).toBe(name)
}

/** The translate route is lazy-loaded, so the push settles asynchronously. */
async function expectRoute(name: string): Promise<void> {
  await flushPromises()
  await vi.waitFor(() => expect(router.currentRoute.value.name).toBe(name), { timeout: 2000 })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('opening a tab from another page jumps to 翻译', () => {
  it('showTranslatePage navigates away from 单词本', async () => {
    await setupFreshEnv()
    await goTo('vocabulary')

    showTranslatePage()
    await expectRoute('translate')
  })

  it('clicking a vocabulary card shows the word on the translate page', async () => {
    const { i18n } = await setupFreshEnv()
    await goTo('vocabulary')

    const wrapper = mount(VocabularyCard, {
      props: { item: wordItem() },
      global: { plugins: [i18n] },
    })
    await wrapper.find('.card-body').trigger('click')
    await expectRoute('translate')
    // Stored structured content: the tab renders immediately, no provider call.
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    const tab = tabs.activeTab
    expect(tab).not.toBeNull()
    expect(translation.stateFor(tab!.id).status).toBe('success')
    wrapper.unmount()
  })

  it('opening a history entry from 设置 lands on 翻译', async () => {
    const env = await setupFreshEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()
    const tab = tabs.newTab()
    translation.setInput(tab.id, 'suspend')
    await translation.translate(tab.id)

    await goTo('settings')
    const ui = useUiStore()
    ui.historyOpen = true
    const wrapper = mount(HistoryDrawer, {
      global: { plugins: [env.i18n] },
      attachTo: document.body,
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
    await wrapper.vm.$nextTick()

    const first = document.querySelector('.history-item') as HTMLElement | null
    expect(first).not.toBeNull()
    first!.click()
    await expectRoute('translate')
    wrapper.unmount()
  })
})
