/**
 * improvement bug #5 regression guard: boot hotkey registration.
 *
 * The host reports READY as soon as the sidecar answered — usually before the
 * webview finished resolving the API client. App.vue then called
 * `registerHotkeysFromSettings()` immediately; `settings.load()` hit
 * `useApi()` before the client existed, threw, and the function returned null.
 * Result: on a fresh install NEITHER Ctrl+Alt+Space NOR Ctrl+Alt+Q was ever
 * registered and `hotkeys.json` was never written.
 *
 * These tests pin the ordering: the API client is awaited first, and the
 * hotkey pair is still applied.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const h = vi.hoisted(() => {
  let release: () => void = () => {}
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  return {
    gate,
    state: { initialized: false, failSettings: false },
    release: () => release(),
    appSettings: {
      theme: 'system',
      always_on_top: true,
      auto_start: false,
      hotkey_toggle_window: 'Ctrl+Alt+Space',
      hotkey_quick_translate: 'Ctrl+Alt+Q',
    },
  }
})

vi.mock('@/api', () => ({
  initApi: async () => {
    await h.gate
    h.state.initialized = true
    return {}
  },
  isTauri: () => true,
  isMockMode: () => false,
  useApi: () => {
    if (!h.state.initialized) throw new Error('API client not initialized')
    return {
      getSettings: async () => {
        if (h.state.failSettings) throw new Error('settings unavailable')
        return h.appSettings
      },
      getProviderSettings: async () => ({
        mode: 'deepseek',
        base_url: 'https://api.deepseek.com',
        translation_model: 'deepseek-flash',
        chat_model: 'deepseek-flash',
        api_key_configured: false,
      }),
      getRuntimeCapabilities: async () => ({ context_tokens: 1000000, output_tokens: 8192 }),
      getGlobalContext: async () => ({ content: '' }),
    }
  },
  toApiError: (err: unknown) => ({
    code: 'TEST',
    message: err instanceof Error ? err.message : String(err),
    retryable: false,
  }),
}))

const { registerHotkeysFromSettings, __setHotkeyInvokeForTests } = await import('@/services/hotkeys')

beforeEach(() => {
  setActivePinia(createPinia())
  __setHotkeyInvokeForTests(null)
  h.state.failSettings = false
})

describe('boot hotkey registration (improvement bug #5)', () => {
  it('waits for the API client instead of dropping the registration', async () => {
    const calls: Array<{ cmd: string; args?: Record<string, unknown> }> = []
    __setHotkeyInvokeForTests(async (cmd, args) => {
      calls.push({ cmd, args })
      return { registered: true, conflict: null }
    })

    h.state.initialized = false
    const pending = registerHotkeysFromSettings()
    // Let the function reach its first await: nothing may be applied yet.
    await Promise.resolve()
    await Promise.resolve()
    expect(calls).toHaveLength(0)

    // The sidecar becomes ready and the client resolves.
    h.release()
    const result = await pending

    expect(result).toEqual({ registered: true, conflict: null })
    expect(calls).toEqual([
      {
        cmd: 'apply_hotkeys',
        args: {
          hotkeys: { show_hide: 'Ctrl+Alt+Space', translate_selection: 'Ctrl+Alt+Q' },
        },
      },
    ])
  })

  it('reports the failure instead of returning null silently when settings cannot load', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    __setHotkeyInvokeForTests(async () => ({ registered: true, conflict: null }))
    h.state.failSettings = true

    h.release()
    const result = await registerHotkeysFromSettings()

    expect(result).toBeNull()
    expect(errorSpy).toHaveBeenCalled()
    errorSpy.mockRestore()
  })
})
