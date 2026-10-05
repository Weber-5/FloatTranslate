/**
 * improvement bug #8: the reader asked for a translation input ABOVE the word
 * result so the next lookup happens in place — no new tab — with earlier
 * results still reachable from translation history.
 */
import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TranslatePage from '@/components/translate/TranslatePage.vue'
import { useApi } from '@/api'
import { setupFreshEnv } from './helpers'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'

/** The mock client sleeps 200ms per model call; settle past it. */
async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 260))
  await flushPromises()
}

async function mountTranslated(input: string) {
  const { i18n } = await setupFreshEnv()
  const tabs = useTabsStore()
  const translation = useTranslationStore()
  await tabs.restore()
  const tab = tabs.newTab()
  translation.setInput(tab.id, input)
  await translation.translate(tab.id)
  const wrapper = mount(TranslatePage, { global: { plugins: [i18n] } })
  await flushPromises()
  return { wrapper, tabs, translation, tabId: tab.id }
}

function resultWord(translation: ReturnType<typeof useTranslationStore>, tabId: string): string | null {
  const result = translation.stateFor(tabId).response?.result
  return result && 'word' in result ? result.word : null
}

describe('bug #8: inline translate box above the result', () => {
  it('is absent while the tab is still an empty input tab', async () => {
    const { i18n } = await setupFreshEnv()
    const tabs = useTabsStore()
    await tabs.restore()
    const wrapper = mount(TranslatePage, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="quick-translate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="translate-send"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('sits above the word result once a translation exists', async () => {
    const { wrapper } = await mountTranslated('suspended')

    expect(wrapper.find('[data-testid="quick-translate"]').exists()).toBe(true)
    expect(wrapper.find('.word-result').exists()).toBe(true)
    // The box must come BEFORE the result card in document order.
    const html = wrapper.html()
    expect(html.indexOf('quick-translate-input')).toBeLessThan(html.indexOf('class="word-result'))
    wrapper.unmount()
  })

  it('translates in the current tab instead of opening a new one', async () => {
    const { wrapper, tabs, translation, tabId } = await mountTranslated('suspended')
    const tabsBefore = tabs.tabs.length
    const historyBefore = (await useApi().listHistory()).items.length

    await wrapper.find('[data-testid="quick-translate-input"]').setValue('particular')
    await wrapper.find('[data-testid="quick-translate"]').trigger('submit')
    await settle()

    // Same tab count, and the SAME tab now carries the new word.
    expect(tabs.tabs.length).toBe(tabsBefore)
    expect(tabs.activeTab?.id).toBe(tabId)
    expect(translation.stateFor(tabId).input).toBe('particular')
    expect(resultWord(translation, tabId)).toBe('particular')

    // The previous lookup is still archived in history.
    const history = await useApi().listHistory()
    expect(history.items.length).toBe(historyBefore + 1)
    expect(history.items.some((item) => item.input_text === 'suspended')).toBe(true)
    wrapper.unmount()
  })

  it('Enter submits, clears the field and replaces the result', async () => {
    const { wrapper, translation, tabId } = await mountTranslated('suspended')

    const field = wrapper.find('[data-testid="quick-translate-input"]')
    await field.setValue('distinctive')
    await field.trigger('keydown.enter')
    await settle()

    expect((field.element as HTMLInputElement).value).toBe('')
    expect(resultWord(translation, tabId)).toBe('distinctive')
    wrapper.unmount()
  })

  it('disables the field while the request is in flight', async () => {
    const { wrapper } = await mountTranslated('suspended')

    await wrapper.find('[data-testid="quick-translate-input"]').setValue('distinctive')
    await wrapper.find('[data-testid="quick-translate"]').trigger('submit')
    await flushPromises() // status flipped to translating synchronously

    expect(wrapper.find('[data-testid="quick-translate-input"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="quick-translate-send"]').attributes('disabled')).toBeDefined()

    await settle()
    expect(wrapper.find('[data-testid="quick-translate-input"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps the previous result visible when the new lookup fails', async () => {
    const { wrapper, translation, tabId } = await mountTranslated('suspended')

    await wrapper.find('[data-testid="quick-translate-input"]').setValue('trigger-error')
    await wrapper.find('[data-testid="quick-translate"]').trigger('submit')
    await settle()

    expect(translation.stateFor(tabId).status).toBe('retryable_error')
    expect(resultWord(translation, tabId)).toBe('suspended')
    expect(wrapper.find('[data-testid="translate-error-banner"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('retry after a failed inline lookup re-runs the TYPED text, not the old result', async () => {
    const { wrapper, translation, tabId } = await mountTranslated('suspended')
    const api = useApi()
    const createSpy = vi.spyOn(api, 'createTranslation')
    const retranslateSpy = vi.spyOn(api, 'retranslate')

    await wrapper.find('[data-testid="quick-translate-input"]').setValue('trigger-error')
    await wrapper.find('[data-testid="quick-translate"]').trigger('submit')
    await settle()
    expect(translation.stateFor(tabId).status).toBe('retryable_error')

    createSpy.mockClear()
    retranslateSpy.mockClear()
    await wrapper.find('.banner-retry').trigger('click')
    await flushPromises()

    expect(retranslateSpy).not.toHaveBeenCalled()
    expect(createSpy).toHaveBeenCalledWith('trigger-error', undefined, false)
    await settle()
    wrapper.unmount()
  })
})
