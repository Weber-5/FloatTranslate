import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import TextResult from '@/components/translate/TextResult.vue'
import type { TextTranslation, TranslationResponse } from '@/api/types'

function longTextResponse(segments: { source: string; translation: string }[]): TranslationResponse {
  return {
    translation_id: 'tr-long-text',
    kind: 'text',
    source: 'model',
    result: {
      source_markdown: segments.map((segment) => segment.source).join('\n\n'),
      translated_markdown: segments.map((segment) => segment.translation).join('\n\n'),
      segments,
    } satisfies TextTranslation,
    model: 'deepseek-flash',
    created_at: new Date().toISOString(),
  }
}

describe('text result reading polish (Phase 3)', () => {
  afterEach(() => {
    Reflect.deleteProperty(navigator as unknown as { clipboard?: unknown }, 'clipboard')
  })

  it('renders 8+ segments with a copy button per segment and one for all', async () => {
    const { i18n } = await setupTestEnv()
    const segments = Array.from({ length: 8 }, (_, index) => ({
      source: `Paragraph number ${index + 1} shares the latest research findings.`,
      translation: `第 ${index + 1} 段的中文译文内容。`,
    }))
    const wrapper = mount(TextResult, {
      props: { response: longTextResponse(segments) },
      global: { plugins: [i18n] },
    })

    expect(wrapper.findAll('.text-pair')).toHaveLength(8)
    expect(wrapper.findAll('[data-testid="segment-copy"]')).toHaveLength(8)
    expect(wrapper.find('[data-testid="text-copy-all"]').exists()).toBe(true)
  })

  it('copies the segment translation and the whole translated result', async () => {
    const { i18n } = await setupTestEnv()
    const writeText = vi.fn(async () => undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    const segments = [
      { source: 'First paragraph.', translation: '第一段译文。' },
      { source: 'Second paragraph.', translation: '第二段译文。' },
      {
        source: 'A paragraph with a very long protected URL https://example.com/some/very/long/path that must wrap instead of scrolling.',
        translation: '包含长 URL 的段落 https://example.com/some/very/long/path 不应横向溢出。',
      },
    ]
    const response = longTextResponse(segments)
    const wrapper = mount(TextResult, {
      props: { response },
      global: { plugins: [i18n] },
    })

    const segmentButtons = wrapper.findAll('[data-testid="segment-copy"]')
    await segmentButtons[1].trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('第二段译文。')
    // Transient feedback on the copied button.
    expect(segmentButtons[1].text()).toContain('已复制')

    await wrapper.find('[data-testid="text-copy-all"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith(
      (response.result as TextTranslation).translated_markdown,
    )
  })

  it('keeps token clickability alongside the copy actions', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(TextResult, {
      props: {
        response: longTextResponse([
          { source: 'The rabbit is running quickly.', translation: '兔子正在跑。' },
        ]),
      },
      global: { plugins: [i18n] },
    })
    const token = wrapper.find('[data-token="running"]')
    expect(token.exists()).toBe(true)
    await token.trigger('click')
    const tabs = (await import('@/stores/tabs')).useTabsStore()
    expect(tabs.tabs.some((tab) => tab.kind === 'word' && tab.title === 'run')).toBe(true)
  })
})
