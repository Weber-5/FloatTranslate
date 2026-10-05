/**
 * improvement bug #8: the page used to render an extra page-level Ask AI
 * below the result card, duplicating the one in the word status bar while
 * text results had none at all. The single Ask AI now lives in each result's
 * own toolbar: WordResult status bar and TextResult toolbar.
 */
import { describe, expect, it } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TranslatePage from '@/components/translate/TranslatePage.vue'
import TextResult from '@/components/translate/TextResult.vue'
import type { TranslationResponse } from '@/api/types'
import { setupTestEnv } from './helpers'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'

function textResponse(): TranslationResponse {
  return {
    translation_id: '01HW0TEXT0RESULT0000000000X',
    kind: 'text',
    source: 'model',
    model: 'deepseek-flash',
    created_at: '2026-10-05T00:00:00Z',
    result: {
      source_markdown: 'The studies keep running.',
      translated_markdown: '这些研究仍在继续。',
      segments: [{ source: 'The studies keep running.', translation: '这些研究仍在继续。' }],
    },
  } as TranslationResponse
}

async function mountTranslated(input: string) {
  const { i18n } = await setupTestEnv()
  const tabs = useTabsStore()
  const translation = useTranslationStore()
  await tabs.restore()
  const tab = tabs.newTab()
  translation.setInput(tab.id, input)
  await translation.translate(tab.id)
  const wrapper = mount(TranslatePage, { global: { plugins: [i18n] } })
  await flushPromises()
  return wrapper
}

function askAiButtons(wrapper: ReturnType<typeof mount>): number {
  return wrapper.findAll('button').filter((b) => b.text().includes('Ask AI')).length
}

describe('bug #8: exactly one Ask AI per result view', () => {
  it('word result renders exactly one Ask AI (status bar only)', async () => {
    const wrapper = await mountTranslated('suspended')
    expect(askAiButtons(wrapper)).toBe(1)
    wrapper.unmount()
  })

  it('text result exposes Ask AI in its toolbar', async () => {
    const wrapper = await mountTranslated('the quick brown fox')
    expect(askAiButtons(wrapper)).toBe(1)
    expect(wrapper.find('[data-testid="text-copy-all"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('TextResult mounts standalone with a toolbar Ask AI', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(TextResult, {
      props: { response: textResponse() },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('.toolbar-actions').exists()).toBe(true)
    expect(wrapper.text()).toContain('Ask AI')
    wrapper.unmount()
  })
})
