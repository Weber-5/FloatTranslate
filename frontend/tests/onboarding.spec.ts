import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { setupFreshEnv } from './helpers'
import { useSettingsStore } from '@/stores/settings'
import { getClient } from '@/api'
import OnboardingWizard from '@/components/onboarding/OnboardingWizard.vue'

async function mountWizard() {
  const { i18n } = await setupFreshEnv()
  // Fresh install: no API key configured, so the wizard is required.
  const api = await getClient()
  await api.resetApp()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/onboarding', name: 'onboarding', component: OnboardingWizard },
      { path: '/translate', name: 'translate', component: { template: '<div />' } },
      { path: '/settings', name: 'settings', component: { template: '<div />' } },
      { path: '/', redirect: '/translate' },
    ],
  })
  await router.push('/onboarding')
  await router.isReady()
  const wrapper = mount(OnboardingWizard, {
    global: { plugins: [i18n, router] },
    attachTo: document.body,
  })
  await wrapper.vm.$nextTick()
  return { wrapper, router, i18n }
}

describe('onboarding real flow (docs/00 §10)', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('tests with the CURRENT form values, persists on success, then enters the main UI', async () => {
    const { wrapper, router } = await mountWizard()
    const api = await getClient()
    const saveSpy = vi.spyOn(api, 'updateProviderSettings')
    const testSpy = vi.spyOn(api, 'testProviderConnection')
    const settings = useSettingsStore()
    await settings.load(true)
    expect(settings.provider?.api_key_configured).toBe(false)

    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('sk-onboarding-key')
    await wrapper.find('[data-testid="onboarding-base-url"]').setValue('https://api.deepseek.com')
    await wrapper.find('[data-testid="onboarding-test"]').trigger('click')
    await vi.waitFor(() => {
      expect(router.currentRoute.value.name).toBe('translate')
    })

    // The submitted config was tested…
    expect(testSpy).toHaveBeenCalledTimes(1)
    const tested = testSpy.mock.calls[0][0]
    expect(tested.mode).toBe('deepseek')
    expect(tested.base_url).toBe('https://api.deepseek.com')
    expect(tested.api_key).toBe('sk-onboarding-key')
    // …and persisted via PUT /settings/provider.
    expect(saveSpy).toHaveBeenCalledTimes(1)
    expect(settings.provider?.api_key_configured).toBe(true)
    wrapper.unmount()
  })

  it('stays on the wizard with an inline reason when the test fails', async () => {
    const { wrapper, router } = await mountWizard()
    const api = await getClient()
    const saveSpy = vi.spyOn(api, 'updateProviderSettings')

    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('invalid-key')
    await wrapper.find('[data-testid="onboarding-test"]').trigger('click')
    await vi.waitFor(() => {
      expect(wrapper.find('[data-testid="onboarding-error"]').exists()).toBe(true)
    })

    expect(wrapper.text()).toContain('Mock：API Key 无效')
    expect(router.currentRoute.value.name).toBe('onboarding')
    // A failed test must NOT persist anything.
    expect(saveSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires the API key before anything is sent', async () => {
    const { wrapper } = await mountWizard()
    const api = await getClient()
    const testSpy = vi.spyOn(api, 'testProviderConnection')

    await wrapper.find('[data-testid="onboarding-test"]').trigger('click')
    await vi.waitFor(() => {
      expect(wrapper.find('[data-testid="onboarding-error"]').exists()).toBe(true)
    })

    expect(testSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
