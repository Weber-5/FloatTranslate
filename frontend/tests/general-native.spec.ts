import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { setupFreshEnv } from './helpers'
import { getClient } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import {
  __setNativeInvokeForTests,
  getAutostart,
  setAutostart,
  setAlwaysOnTop,
  openLogsDir,
  resetWindowState,
} from '@/services/native'
import GeneralSection from '@/components/settings/GeneralSection.vue'

type NativeCall = { cmd: string; args?: Record<string, unknown> }

function recordNative(results: Record<string, (args?: Record<string, unknown>) => unknown>): {
  calls: NativeCall[]
} {
  const calls: NativeCall[] = []
  __setNativeInvokeForTests(async (cmd, args) => {
    calls.push({ cmd, args })
    const handler = results[cmd]
    if (!handler) throw new Error(`command missing: ${cmd}`)
    return handler(args)
  })
  return { calls }
}

async function mountGeneral(alwaysOnTopInitial = false): Promise<{ wrapper: VueWrapper; calls: NativeCall[] }> {
  const { i18n } = await setupFreshEnv()
  const settings = useSettingsStore()
  await settings.load()
  // The mock defaults always_on_top to true; start from false so toggling ON
  // actually fires a change event (test-utils skips no-op checkbox changes).
  if (alwaysOnTopInitial !== (settings.app?.always_on_top ?? false)) {
    await settings.saveApp({ always_on_top: alwaysOnTopInitial })
  }
  const { calls } = recordNative({
    get_autostart: () => false,
    set_autostart: (args) => (args?.enabled as boolean),
    set_always_on_top: () => undefined,
  })
  const wrapper = mount(GeneralSection, { global: { plugins: [i18n] } })
  await flushPromises()
  return { wrapper, calls }
}

afterEach(() => {
  __setNativeInvokeForTests(null)
  vi.restoreAllMocks()
})

describe('GeneralSection native toggles (Phase 5)', () => {
  it('always-on-top invokes the host command first, then saves the setting', async () => {
    const { wrapper, calls } = await mountGeneral()
    const settings = useSettingsStore()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')

    await wrapper.find('[data-testid="always-on-top"]').setValue(true)
    await flushPromises()

    const aotCall = calls.find((call) => call.cmd === 'set_always_on_top')
    expect(aotCall?.args).toEqual({ enabled: true })
    expect(putSpy).toHaveBeenCalledTimes(1)
    expect((putSpy.mock.calls[0][0] as Record<string, unknown>).always_on_top).toBe(true)
    expect(settings.app?.always_on_top).toBe(true)
    wrapper.unmount()
  })

  it('auto start syncs the boot state from get_autostart and saves on toggle', async () => {
    const { wrapper, calls } = await mountGeneral()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')

    // get_autostart ran on mount (boot sync).
    expect(calls.some((call) => call.cmd === 'get_autostart')).toBe(true)

    await wrapper.find('[data-testid="auto-start"]').setValue(true)
    await flushPromises()

    const startCall = calls.find((call) => call.cmd === 'set_autostart')
    expect(startCall?.args).toEqual({ enabled: true })
    expect(putSpy).toHaveBeenCalledTimes(1)
    expect((putSpy.mock.calls[0][0] as Record<string, unknown>).auto_start).toBe(true)
    wrapper.unmount()
  })

  it('missing autostart command disables the toggle with a tooltip', async () => {
    const { i18n } = await setupFreshEnv()
    const settings = useSettingsStore()
    await settings.load()
    __setNativeInvokeForTests(async () => {
      throw new Error('command missing')
    })
    const wrapper = mount(GeneralSection, { global: { plugins: [i18n] } })
    await flushPromises()

    const box = wrapper.find('[data-testid="auto-start"]')
    expect((box.element as HTMLInputElement).disabled).toBe(true)
    // Tooltip on the switch label.
    expect(wrapper.text()).toContain('无法读取系统开机启动状态')
    wrapper.unmount()
  })

  it('failing always-on-top command shows the unsupported notice and does not save', async () => {
    const { i18n } = await setupFreshEnv()
    const settings = useSettingsStore()
    await settings.load()
    await settings.saveApp({ always_on_top: false })
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')
    __setNativeInvokeForTests(async (cmd) => {
      if (cmd === 'get_autostart') return false
      throw new Error('command missing')
    })
    const wrapper = mount(GeneralSection, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.find('[data-testid="always-on-top"]').setValue(true)
    await flushPromises()

    expect(putSpy).not.toHaveBeenCalled()
    expect(settings.app?.always_on_top).toBe(false)
    const notice = wrapper.find('[data-testid="general-notice"]')
    expect(notice.exists()).toBe(true)
    expect(notice.text()).toContain('当前环境不支持')
    // The switch reverts to the previous value.
    expect((wrapper.find('[data-testid="always-on-top"]').element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })
})

describe('native service wrappers (frozen command shapes)', () => {
  it('forwards the frozen command names and payloads', async () => {
    const { calls } = recordNative({
      get_autostart: () => true,
      set_autostart: () => true,
      set_always_on_top: () => undefined,
      reset_window_state: () => undefined,
      open_logs_dir: () => undefined,
    })
    await expect(getAutostart()).resolves.toEqual({ ok: true, value: true })
    await expect(setAutostart(true)).resolves.toEqual({ ok: true, value: true })
    await expect(setAlwaysOnTop(false)).resolves.toEqual({ ok: true, value: false })
    await expect(resetWindowState()).resolves.toEqual({ ok: true, value: true })
    await expect(openLogsDir()).resolves.toEqual({ ok: true, value: true })

    expect(calls.map((call) => call.cmd)).toEqual([
      'get_autostart',
      'set_autostart',
      'set_always_on_top',
      'reset_window_state',
      'open_logs_dir',
    ])
    expect(calls[1].args).toEqual({ enabled: true })
    expect(calls[2].args).toEqual({ enabled: false })
  })

  it('pick_save_path normalizes ok/cancelled and host errors to unsupported', async () => {
    __setNativeInvokeForTests(async (cmd, args) => {
      expect(cmd).toBe('pick_save_path')
      expect((args?.options as { default_file_name?: string }).default_file_name).toBe('a.json')
      return { ok: true, path: 'C:\\pick\\a.json' }
    })
    const picked = await import('@/services/native').then((m) =>
      m.pickSavePath({ default_file_name: 'a.json', filter_name: 'JSON', filter_ext: 'json' }),
    )
    expect(picked).toEqual({ ok: true, path: 'C:\\pick\\a.json' })

    __setNativeInvokeForTests(async () => ({ ok: false, cancelled: true }))
    const cancelled = await import('@/services/native').then((m) => m.pickSavePath({}))
    expect(cancelled).toEqual({ ok: false, cancelled: true })

    __setNativeInvokeForTests(async () => {
      throw new Error('missing')
    })
    const unsupported = await import('@/services/native').then((m) => m.pickSavePath({}))
    expect(unsupported).toEqual({ ok: false, cancelled: true, unsupported: true })
    __setNativeInvokeForTests(null)
  })

  it('pick_open_path in pure mock mode falls back to prompt()', async () => {
    const promptSpy = vi.spyOn(window, 'prompt').mockReturnValue('C:\\mock\\backup.json')
    const result = await import('@/services/native').then((m) => m.pickOpenPath({}))
    expect(result).toEqual({ ok: true, path: 'C:\\mock\\backup.json' })
    expect(promptSpy).toHaveBeenCalled()
    promptSpy.mockRestore()
  })
})
