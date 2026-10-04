import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import { useSettingsStore } from '@/stores/settings'
import { getClient } from '@/api'
import ProviderSection from '@/components/settings/ProviderSection.vue'

describe('provider api key handling', () => {
  it('clears the key input after save, reports configured=true, and never renders plaintext', async () => {
    const { i18n } = await setupTestEnv()
    const api = await getClient()
    // Simulate a fresh install: no API key configured yet.
    await api.resetApp()

    const settings = useSettingsStore()
    await settings.load(true)
    expect(settings.provider?.api_key_configured).toBe(false)

    const wrapper = mount(ProviderSection, {
      global: { plugins: [i18n] },
    })
    await settings.load(true)
    await wrapper.vm.$nextTick()

    const keyInput = wrapper.find('[data-testid="provider-api-key-input"]')
    expect(keyInput.exists()).toBe(true)
    // The input must never be prefetched with plaintext.
    expect((keyInput.element as HTMLInputElement).value).toBe('')

    const secret = 'sk-secret-12345-abcdef'
    await keyInput.setValue(secret)
    expect((keyInput.element as HTMLInputElement).type).toBe('password')

    await wrapper.find('[data-testid="provider-save"]').trigger('click')
    await settings.load(true)
    await wrapper.vm.$nextTick()

    expect(settings.provider?.api_key_configured).toBe(true)
    // Input is cleared after save...
    const inputAfter = wrapper.find('[data-testid="provider-api-key-input"]')
    expect((inputAfter.element as HTMLInputElement).value).toBe('')
    // ...and the plaintext never appears anywhere in the rendered DOM.
    expect(wrapper.html()).not.toContain(secret)
  })
})
