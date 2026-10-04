/**
 * Custom frameless titlebar (docs/03 §2): the window can only be dragged when
 * the element under the mouse carries `data-tauri-drag-region`. TabBar fills
 * the whole titlebar, so its ROOT must be a drag region — regression guard
 * for the "无法拖动" bug (tabs/buttons stay interactive, i.e. NOT drag
 * regions).
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount } from '@vue/test-utils'
import TabBar from '@/components/layout/TabBar.vue'
import TitleBar from '@/components/layout/TitleBar.vue'
import { setupTestEnv } from './helpers'

const ROOT = join(__dirname, '..')

function readSource(rel: string): string {
  return readFileSync(join(ROOT, 'src', rel), 'utf8')
}

describe('frameless titlebar drag regions', () => {
  it('TitleBar root is a drag region', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(TitleBar, { global: { plugins: [i18n] } })
    expect(wrapper.find('header.titlebar').attributes('data-tauri-drag-region')).toBeDefined()
  })

  it('TabBar root is a drag region (it fills the titlebar)', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(TabBar, { global: { plugins: [i18n] } })
    expect(wrapper.find('[data-testid="tab-bar"]').attributes('data-tauri-drag-region')).toBeDefined()
  })

  it('interactive tab elements are NOT window drag regions (clicks must work)', async () => {
    const { i18n } = await setupTestEnv()
    const { useTabsStore } = await import('@/stores/tabs')
    useTabsStore().newTab()
    const wrapper = mount(TabBar, { global: { plugins: [i18n] } })
    const tab = wrapper.find('[data-testid="tab-item"]')
    expect(tab.exists()).toBe(true)
    expect(tab.attributes('data-tauri-drag-region')).toBeUndefined()
  })

  it('source audit: no titlebar-area component lost the attribute', () => {
    // Byte-level guard in case a refactor moves the attribute out of a
    // template (mount tests above would still catch the two components).
    expect(readSource('components/layout/TitleBar.vue')).toContain('data-tauri-drag-region')
    expect(readSource('components/layout/TabBar.vue')).toContain('data-tauri-drag-region')
  })

  it('onboarding wizard exposes a drag strip (bare route has no titlebar)', () => {
    expect(readSource('components/onboarding/OnboardingWizard.vue')).toContain(
      'data-tauri-drag-region',
    )
  })
})
