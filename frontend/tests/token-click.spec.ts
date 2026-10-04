import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import { useTabsStore } from '@/stores/tabs'
import type { TextTranslation, WordTranslation } from '@/api/types'
import TextResult from '@/components/translate/TextResult.vue'
import WordResult from '@/components/translate/WordResult.vue'
import { lemmatize } from '@/lib/lemmatize'

function wordResponse(word: string, synonyms: string[]) {
  return {
    translation_id: 'tr-word-test',
    kind: 'word' as const,
    source: 'model' as const,
    result: {
      word,
      lemma: word,
      phonetic_uk: 'test',
      phonetic_us: 'test',
      parts_of_speech: [{ part: 'noun', meanings: ['测试释义'] }],
      synonyms,
      inflections: [word],
    } satisfies WordTranslation,
    model: 'deepseek-flash',
    created_at: new Date().toISOString(),
  }
}

describe('token click drilling', () => {
  it('lemmatizes inflected forms client-side', () => {
    expect(lemmatize('running')).toBe('run')
    expect(lemmatize('suspended')).toBe('suspend')
    expect(lemmatize('studies')).toBe('study')
    expect(lemmatize('dog')).toBe('dog')
  })

  it('clicking token "running" in TextResult opens a word tab titled "run"', async () => {
    const { i18n } = await setupTestEnv()
    const response = {
      translation_id: 'tr-text-test',
      kind: 'text' as const,
      source: 'model' as const,
      result: {
        source_markdown: 'The rabbit is running quickly.',
        translated_markdown: '兔子正在跑。',
        segments: [{ source: 'The rabbit is running quickly.', translation: '兔子正在跑。' }],
      } satisfies TextTranslation,
      model: 'deepseek-flash',
      created_at: new Date().toISOString(),
    }

    const wrapper = mount(TextResult, {
      props: { response },
      global: { plugins: [i18n] },
    })

    const token = wrapper.find('[data-token="running"]')
    expect(token.exists()).toBe(true)
    await token.trigger('click')

    const tabs = useTabsStore()
    expect(tabs.tabs.some((tab) => tab.kind === 'word' && tab.title === 'run')).toBe(true)
  })

  it('clicking synonym chip "paused" opens a word tab titled "paused"', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(WordResult, {
      props: { response: wordResponse('suspend', ['paused', 'halted']) },
      global: { plugins: [i18n] },
    })

    const chips = wrapper.findAll('[data-testid="synonym-chip"]')
    expect(chips.length).toBe(2)
    const pausedChip = chips.find((chip) => chip.text() === 'paused')
    if (!pausedChip) throw new Error('synonym chip "paused" not found')
    await pausedChip.trigger('click')

    const tabs = useTabsStore()
    expect(tabs.tabs.some((tab) => tab.kind === 'word' && tab.title === 'paused')).toBe(true)
  })
})
