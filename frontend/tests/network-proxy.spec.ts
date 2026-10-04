import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { setupFreshEnv } from './helpers'
import { getClient } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import { isValidProxyUrl, requiredProxyScheme } from '@/lib/proxy'
import NetworkSection from '@/components/settings/NetworkSection.vue'

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('proxy URL validation (lib)', () => {
  it('requires the scheme to match the mode', () => {
    expect(isValidProxyUrl('http', 'http://127.0.0.1:8080')).toBe(true)
    expect(isValidProxyUrl('https', 'https://proxy.corp:8443')).toBe(true)
    expect(isValidProxyUrl('socks5', 'socks5://user@10.0.0.1:1080')).toBe(true)
    expect(isValidProxyUrl('http', 'https://127.0.0.1:8080')).toBe(false)
    expect(isValidProxyUrl('https', 'http://proxy.corp')).toBe(false)
    expect(isValidProxyUrl('socks5', 'http://10.0.0.1:1080')).toBe(false)
    expect(isValidProxyUrl('http', 'not a url')).toBe(false)
    expect(isValidProxyUrl('http', 'http://')).toBe(false)
  })

  it('system/none carry no URL constraint', () => {
    expect(requiredProxyScheme('system')).toBeNull()
    expect(requiredProxyScheme('none')).toBeNull()
    expect(requiredProxyScheme('http')).toBe('http:')
    expect(isValidProxyUrl('system', '')).toBe(true)
    expect(isValidProxyUrl('none', 'whatever')).toBe(true)
  })
})

describe('NetworkSection proxy UI (Phase 5 contract)', () => {
  async function mountNetwork(): Promise<{ wrapper: VueWrapper; putCalls: () => Record<string, unknown>[] }> {
    const { i18n } = await setupFreshEnv()
    const settings = useSettingsStore()
    await settings.load()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')
    const wrapper = mount(NetworkSection, { global: { plugins: [i18n] } })
    await flushPromises()
    return {
      wrapper,
      putCalls: () => putSpy.mock.calls.map((call) => call[0] as Record<string, unknown>),
    }
  }

  function modeRadios(wrapper: VueWrapper) {
    return wrapper.findAll('input[name="proxy-mode"]')
  }

  it('renders five modes (system/none/http/https/socks5) with system selected by default', async () => {
    const { wrapper } = await mountNetwork()
    const radios = modeRadios(wrapper)
    expect(radios.length).toBe(5)
    expect((radios[0].element as HTMLInputElement).checked).toBe(true)
    // URL field hidden for system mode.
    expect(wrapper.find('[data-testid="proxy-url-wrap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('switching mode persists immediately and shows the URL field for custom modes', async () => {
    const { wrapper, putCalls } = await mountNetwork()
    const radios = modeRadios(wrapper)

    await radios[2].setValue() // http
    expect(putCalls().some((payload) => payload.proxy_mode === 'http')).toBe(true)
    expect(wrapper.find('[data-testid="proxy-url-wrap"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('an invalid URL shows the inline error and is never saved', async () => {
    vi.useFakeTimers()
    const { wrapper, putCalls } = await mountNetwork()
    await modeRadios(wrapper)[3].setValue() // https
    await flushPromises()

    const url = wrapper.find('[data-testid="proxy-url"]')
    await url.setValue('http://wrong-scheme:8080')
    await url.trigger('change')
    await vi.advanceTimersByTimeAsync(800)

    expect(wrapper.find('[data-testid="proxy-url-error"]').exists()).toBe(true)
    // saveApp PUTs the merged settings object, so assert on the VALUE: the
    // invalid URL never reaches any PUT.
    expect(putCalls().some((payload) => payload.proxy_url === 'http://wrong-scheme:8080')).toBe(false)
    wrapper.unmount()
  })

  it('a valid URL auto-saves through the debounced settings save', async () => {
    vi.useFakeTimers()
    const { wrapper, putCalls } = await mountNetwork()
    await modeRadios(wrapper)[4].setValue() // socks5
    await flushPromises()

    const url = wrapper.find('[data-testid="proxy-url"]')
    await url.setValue('socks5://127.0.0.1:1080')
    await url.trigger('change')
    // Still pending before the debounce window elapses.
    expect(putCalls().some((payload) => payload.proxy_url === 'socks5://127.0.0.1:1080')).toBe(false)
    await vi.advanceTimersByTimeAsync(800)

    const saved = putCalls().find((payload) => payload.proxy_url === 'socks5://127.0.0.1:1080')
    expect(saved).toBeDefined()
    wrapper.unmount()
  })

  it('switching back to system/none hides the URL field', async () => {
    const { wrapper } = await mountNetwork()
    const radios = modeRadios(wrapper)
    await radios[1].setValue() // none
    expect(wrapper.find('[data-testid="proxy-url-wrap"]').exists()).toBe(false)
    await radios[4].setValue() // socks5
    expect(wrapper.find('[data-testid="proxy-url-wrap"]').exists()).toBe(true)
    await radios[0].setValue() // system
    expect(wrapper.find('[data-testid="proxy-url-wrap"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
