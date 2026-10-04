import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ApiError, getClient } from '@/api'
import { setupFreshEnv } from './helpers'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import TranslatePage from '@/components/translate/TranslatePage.vue'

async function mountTranslatePage() {
  const { i18n } = await setupFreshEnv()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/translate', name: 'translate', component: TranslatePage },
      { path: '/settings', name: 'settings', component: { template: '<div />' } },
      { path: '/', redirect: '/translate' },
    ],
  })
  await router.push('/translate')
  await router.isReady()
  const wrapper = mount(TranslatePage, {
    global: { plugins: [i18n, router] },
  })
  await wrapper.vm.$nextTick()
  return { wrapper, router, i18n }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('translate page error variants and cache badge (Phase 2 contract)', () => {
  it('shows the 本地缓存 cache banner when source === cache', async () => {
    const { wrapper } = await mountTranslatePage()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const first = tabs.newTab()
    translation.setInput(first.id, 'the quick brown fox')
    await translation.translate(first.id)
    expect(translation.stateFor(first.id).status).toBe('success')

    const second = tabs.newTab()
    translation.setInput(second.id, 'the quick brown fox')
    await translation.translate(second.id)
    expect(translation.stateFor(second.id).status).toBe('cache_success')
    await wrapper.vm.$nextTick()

    const banner = wrapper.find('[data-testid="cache-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('本地缓存')
    wrapper.unmount()
  })

  it('shows UNSUPPORTED_LANGUAGE inline with the backend message and NO retry button', async () => {
    const { wrapper } = await mountTranslatePage()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, '你好，世界')
    await translation.translate(tab.id)
    await wrapper.vm.$nextTick()

    const banner = wrapper.find('[data-testid="translate-error-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('FloatTranslate 1.0 暂仅支持英译中')
    expect(wrapper.find('.banner-retry').exists()).toBe(false)
    expect(wrapper.find('[data-testid="go-to-settings"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the 前往设置 CTA for PROVIDER_NOT_CONFIGURED and routes to the provider section', async () => {
    const { wrapper, router } = await mountTranslatePage()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    const state = translation.stateFor(tab.id)
    state.error = {
      code: 'PROVIDER_NOT_CONFIGURED',
      message: '尚未配置模型服务',
      retryable: false,
    }
    state.status = 'retryable_error'
    await wrapper.vm.$nextTick()

    const cta = wrapper.find('[data-testid="go-to-settings"]')
    expect(cta.exists()).toBe(true)
    expect(wrapper.find('.banner-retry').exists()).toBe(false)
    await cta.trigger('click')
    await vi.waitFor(() => {
      expect(router.currentRoute.value.path).toBe('/settings')
    })
    expect(router.currentRoute.value.query.section).toBe('provider')
    wrapper.unmount()
  })

  it('offers a retry button for retryable errors', async () => {
    const { wrapper } = await mountTranslatePage()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'please trigger-error now')
    await translation.translate(tab.id)
    await wrapper.vm.$nextTick()

    const banner = wrapper.find('[data-testid="translate-error-banner"]')
    expect(banner.exists()).toBe(true)
    expect(wrapper.find('.banner-retry').exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('retranslate semantics (store level)', () => {
  it('replaces the result on success and keeps the old result on failure', async () => {
    await setupFreshEnv()
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()

    const tab = tabs.newTab()
    translation.setInput(tab.id, 'suspend')
    await translation.translate(tab.id)
    const first = translation.stateFor(tab.id).response
    expect(first).not.toBeNull()
    const firstId = first?.translation_id

    // Failure: keep the previous result visible, surface a retryable error.
    const api = await getClient()
    const spy = vi
      .spyOn(api, 'retranslate')
      .mockRejectedValueOnce(new ApiError('PROVIDER_UNAVAILABLE', 'provider boom', true))
    await translation.retranslate(tab.id)
    let state = translation.stateFor(tab.id)
    expect(state.status).toBe('retryable_error')
    expect(state.error?.retryable).toBe(true)
    expect(state.response?.translation_id).toBe(firstId)
    expect(state.response?.result).toEqual(first?.result)
    spy.mockRestore()

    // Success: the result is replaced with a fresh one.
    await translation.retranslate(tab.id)
    state = translation.stateFor(tab.id)
    expect(state.status).toBe('success')
    expect(state.response?.translation_id).not.toBe(firstId)
    expect(state.response?.source).toBe('model')
  })
})
