import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupFreshEnv } from './helpers'
import { getClient } from '@/api'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import { useUiStore } from '@/stores/ui'
import HistoryDrawer from '@/components/layout/HistoryDrawer.vue'

type Env = Awaited<ReturnType<typeof setupFreshEnv>>

async function openDrawer(env: Env) {
  const tabs = useTabsStore()
  const translation = useTranslationStore()
  await tabs.restore()

  // One word model entry + one cached repeat → both source kinds in history.
  const first = tabs.newTab()
  translation.setInput(first.id, 'suspend')
  await translation.translate(first.id)
  const repeat = tabs.newTab()
  translation.setInput(repeat.id, 'suspend')
  await translation.translate(repeat.id)
  expect(translation.stateFor(repeat.id).status).toBe('cache_success')

  const ui = useUiStore()
  ui.historyOpen = true
  const wrapper = mount(HistoryDrawer, {
    global: { plugins: [env.i18n] },
    attachTo: document.body,
  })
  await new Promise((resolve) => setTimeout(resolve, 20))
  await wrapper.vm.$nextTick()
  return { wrapper, ui, tabs, translation }
}

function drawerEl(selector: string): HTMLElement[] {
  return Array.from(document.querySelectorAll(selector))
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('history drawer real behavior (Phase 2 contract)', () => {
  it('renders kind rows with a source badge (模型 / 本地缓存) per item', async () => {
    const env = await setupFreshEnv()
    const { wrapper, ui } = await openDrawer(env)

    const badges = drawerEl('[data-testid="history-source-badge"]').map((el) =>
      el.textContent?.trim(),
    )
    expect(badges).toContain('本地缓存')
    expect(badges).toContain('模型')
    ui.historyOpen = false
    wrapper.unmount()
  })

  it('deletes a single item via DELETE /history/{id} without touching others', async () => {
    const env = await setupFreshEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    // Distinct inputs → distinct history ids (no cache-duplicate ambiguity).
    const a = tabs.newTab()
    translation.setInput(a.id, 'alpha')
    await translation.translate(a.id)
    const b = tabs.newTab()
    translation.setInput(b.id, 'beta')
    await translation.translate(b.id)

    const api = await getClient()
    const before = (await api.listHistory()).items.length
    expect(before).toBe(2)

    const ui = useUiStore()
    ui.historyOpen = true
    const wrapper = mount(HistoryDrawer, {
      global: { plugins: [env.i18n] },
      attachTo: document.body,
    })
    await new Promise((resolve) => setTimeout(resolve, 20))

    const deleteSpy = vi.spyOn(api, 'deleteHistoryItem')
    const buttons = drawerEl('.history-row .icon-btn-danger')
    expect(buttons.length).toBe(2)
    buttons[0].click()
    await new Promise((resolve) => setTimeout(resolve, 10))
    // ConfirmModal is teleported; confirm via its danger button.
    const confirm = document.querySelector<HTMLButtonElement>('.modal-actions .btn-danger')
    if (!confirm) throw new Error('confirm button not found')
    confirm.click()
    await new Promise((resolve) => setTimeout(resolve, 20))

    expect(deleteSpy).toHaveBeenCalledTimes(1)
    const after = (await api.listHistory()).items.length
    expect(after).toBe(before - 1)
    ui.historyOpen = false
    wrapper.unmount()
  })

  it('opens an item as a new tab via getTranslation — never a provider call', async () => {
    const env = await setupFreshEnv()
    const { wrapper, ui, tabs, translation } = await openDrawer(env)
    const api = await getClient()
    const createSpy = vi.spyOn(api, 'createTranslation')
    const getSpy = vi.spyOn(api, 'getTranslation')
    const tabCountBefore = tabs.tabs.length

    const firstItem = document.querySelector<HTMLButtonElement>('.history-item')
    if (!firstItem) throw new Error('history item not found')
    firstItem.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
    await wrapper.vm.$nextTick()

    expect(createSpy).not.toHaveBeenCalled()
    expect(getSpy).toHaveBeenCalledTimes(1)
    expect(tabs.tabs.length).toBe(tabCountBefore + 1)
    const newTabId = tabs.activeId as string
    const state = translation.stateFor(newTabId)
    expect(state.response).not.toBeNull()
    expect(state.status === 'success' || state.status === 'cache_success').toBe(true)
    ui.historyOpen = false
    wrapper.unmount()
  })
})
