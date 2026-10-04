import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import zhCN from '@/i18n/zh-CN'
import TranslatePage from '@/components/translate/TranslatePage.vue'
import VocabularyPage from '@/components/vocabulary/VocabularyPage.vue'
import AiSidebar from '@/components/layout/AiSidebar.vue'

type Messages = Record<string, unknown>

function collectLeafKeys(messages: Messages, prefix = ''): string[] {
  const keys: string[] = []
  for (const [key, value] of Object.entries(messages)) {
    const path = prefix.length > 0 ? `${prefix}.${key}` : key
    if (value !== null && typeof value === 'object') {
      keys.push(...collectLeafKeys(value as Messages, path))
    } else {
      keys.push(path)
    }
  }
  return keys
}

describe('i18n coverage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('resolves every zh-CN key (no missing strings)', async () => {
    const { i18n } = await setupTestEnv()
    const keys = collectLeafKeys(zhCN as unknown as Messages)
    expect(keys.length).toBeGreaterThan(80)

    const unresolved = keys.filter((key) => {
      const translated = i18n.global.t(key)
      return translated === key || translated.length === 0
    })
    expect(unresolved).toEqual([])
  })

  it('key components mount without missing-key warnings', async () => {
    const { i18n } = await setupTestEnv()

    const warnings: string[] = []
    const spy = vi.spyOn(console, 'warn').mockImplementation((...args: unknown[]) => {
      warnings.push(args.map(String).join(' '))
    })

    try {
      const translateWrapper = mount(TranslatePage, { global: { plugins: [i18n] } })
      await translateWrapper.vm.$nextTick()
      translateWrapper.unmount()

      const vocabWrapper = mount(VocabularyPage, { global: { plugins: [i18n] } })
      await new Promise((resolve) => setTimeout(resolve, 50))
      await vocabWrapper.vm.$nextTick()
      vocabWrapper.unmount()

      const sidebarWrapper = mount(AiSidebar, { global: { plugins: [i18n] } })
      await new Promise((resolve) => setTimeout(resolve, 50))
      await sidebarWrapper.vm.$nextTick()
      sidebarWrapper.unmount()
    } finally {
      spy.mockRestore()
    }

    const missingKeyWarnings = warnings.filter(
      (message) =>
        /Not found '.+' key in/i.test(message) ||
        /Fall back to (sanitize|the 'en') /i.test(message) ||
        /Intally missing/i.test(message),
    )
    expect(missingKeyWarnings).toEqual([])
  })
})
