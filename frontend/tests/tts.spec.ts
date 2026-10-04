import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import WordResult from '@/components/translate/WordResult.vue'
import { isTtsAvailable, speak } from '@/services/tts'
import type { WordTranslation } from '@/api/types'

function wordResponse(word: string): { translation_id: string; kind: 'word'; source: 'model'; result: WordTranslation; model: string; created_at: string } {
  return {
    translation_id: 'tr-tts-test',
    kind: 'word',
    source: 'model',
    result: {
      word,
      lemma: word,
      phonetic_uk: '/səˈspend/',
      phonetic_us: '/səˈspɛnd/',
      parts_of_speech: [{ part: 'verb', meanings: ['暂停；中止'] }],
      synonyms: ['paused'],
      inflections: ['suspended'],
    },
    model: 'deepseek-flash',
    created_at: new Date().toISOString(),
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('local TTS (docs/00 §4)', () => {
  it('is unavailable in a plain jsdom environment', () => {
    expect(isTtsAvailable()).toBe(false)
  })

  it('disables the speaker buttons with a tooltip when TTS is unavailable', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(WordResult, {
      props: { response: wordResponse('suspend') },
      global: { plugins: [i18n] },
    })
    for (const id of ['speak-uk', 'speak-us']) {
      const button = wrapper.find(`[data-testid="${id}"]`)
      expect(button.exists()).toBe(true)
      expect(button.attributes('disabled')).toBeDefined()
      expect(button.attributes('title')).toBe('当前设备不支持语音朗读')
    }
  })

  it('speaks the WORD itself with the requested accent and rate 1.0', async () => {
    const created: Array<{ text: string; lang: string; rate: number; voice: unknown }> = []
    class FakeUtterance {
      text = ''
      lang = ''
      rate = 1
      voice: unknown = null
      constructor(text: string) {
        this.text = text
        created.push(this)
      }
    }
    const speakFn = vi.fn()
    const cancelFn = vi.fn()
    vi.stubGlobal('speechSynthesis', {
      cancel: cancelFn,
      speak: speakFn,
      getVoices: () => [
        { lang: 'en_US', name: 'English (United States)' },
        { lang: 'en-GB', name: 'British English' },
      ],
    })
    vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)

    expect(isTtsAvailable()).toBe(true)

    speak('suspend', 'en-GB')
    expect(speakFn).toHaveBeenCalledTimes(1)
    expect(cancelFn).toHaveBeenCalled()
    expect(created[0].text).toBe('suspend')
    expect(created[0].lang).toBe('en-GB')
    expect(created[0].rate).toBe(1)
    // en-GB voice preferred over the en_US one.
    expect((created[0].voice as { lang: string }).lang).toBe('en-GB')
  })

  it('keeps speaker buttons enabled and functional when TTS exists', async () => {
    const { i18n } = await setupTestEnv()
    vi.stubGlobal('speechSynthesis', {
      cancel: vi.fn(),
      speak: vi.fn(),
      getVoices: () => [],
    })
    vi.stubGlobal(
      'SpeechSynthesisUtterance',
      class {
        text = ''
        lang = ''
        rate = 1
        voice: unknown = null
        constructor(text: string) {
          this.text = text
        }
      },
    )

    const wrapper = mount(WordResult, {
      props: { response: wordResponse('suspend') },
      global: { plugins: [i18n] },
    })
    const uk = wrapper.find('[data-testid="speak-uk"]')
    const us = wrapper.find('[data-testid="speak-us"]')
    expect(uk.attributes('disabled')).toBeUndefined()
    expect(us.attributes('disabled')).toBeUndefined()
    expect(uk.attributes('title')).toBe('英式发音')
  })
})
