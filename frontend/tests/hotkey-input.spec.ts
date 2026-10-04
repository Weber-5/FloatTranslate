import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupTestEnv, setupFreshEnv } from './helpers'
import HotkeyInput from '@/components/settings/HotkeyInput.vue'
import GeneralSection from '@/components/settings/GeneralSection.vue'
import { useSettingsStore } from '@/stores/settings'
import { getClient } from '@/api'
import {
  applyHotkeys,
  isValidHotkeyFormat,
  registerHotkeysFromSettings,
  __setHotkeyInvokeForTests,
} from '@/services/hotkeys'

afterEach(() => {
  __setHotkeyInvokeForTests(null)
  vi.restoreAllMocks()
})

describe('hotkey recorder component', () => {
  it('shows the current value and records a combo with modifiers', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(HotkeyInput, {
      props: { modelValue: 'Ctrl+Alt+Space', label: 'toggle hotkey' },
      global: { plugins: [i18n] },
    })
    const box = wrapper.find('[data-testid="hotkey-input"]')
    expect(box.text()).toContain('Ctrl+Alt+Space')

    await box.trigger('focus')
    await box.trigger('keydown', { key: 'q', ctrlKey: true, altKey: true })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['Ctrl+Alt+Q'])
  })

  it('normalizes special keys (space) into the canonical format', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(HotkeyInput, {
      props: { modelValue: 'Ctrl+Alt+Q', label: 'toggle hotkey' },
      global: { plugins: [i18n] },
    })
    const box = wrapper.find('[data-testid="hotkey-input"]')
    await box.trigger('focus')
    await box.trigger('keydown', { key: ' ', ctrlKey: true, altKey: true })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['Ctrl+Alt+Space'])
  })

  it('rejects combos without a modifier and shows the inline hint', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(HotkeyInput, {
      props: { modelValue: 'Ctrl+Alt+Q', label: 'toggle hotkey' },
      global: { plugins: [i18n] },
    })
    const box = wrapper.find('[data-testid="hotkey-input"]')
    await box.trigger('focus')
    await box.trigger('keydown', { key: 'a' })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.find('.hotkey-invalid').exists()).toBe(true)
  })

  it('Esc cancels the recording without emitting', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(HotkeyInput, {
      props: { modelValue: 'Ctrl+Alt+Q', label: 'toggle hotkey' },
      global: { plugins: [i18n] },
    })
    const box = wrapper.find('[data-testid="hotkey-input"]')
    await box.trigger('focus')
    await box.trigger('keydown', { key: 'Escape' })
    await box.trigger('keydown', { key: 'q', ctrlKey: true, altKey: true })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    // Recording ended: the current value is displayed again.
    expect(box.text()).toContain('Ctrl+Alt+Q')
  })

  it('Backspace clears the draft so a fresh combo can be recorded', async () => {
    const { i18n } = await setupTestEnv()
    const wrapper = mount(HotkeyInput, {
      props: { modelValue: 'Ctrl+Alt+Q', label: 'toggle hotkey' },
      global: { plugins: [i18n] },
    })
    const box = wrapper.find('[data-testid="hotkey-input"]')
    await box.trigger('focus')
    await box.trigger('keydown', { key: 'Backspace' })
    // Draft cleared -> the recording placeholder is visible.
    expect(box.text()).not.toContain('Ctrl+Alt+Q')
    // A new combo can still be recorded afterwards.
    await box.trigger('keydown', { key: 's', ctrlKey: true, altKey: true })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['Ctrl+Alt+S'])
  })

  it('validates the frozen hotkey string format', () => {
    expect(isValidHotkeyFormat('Ctrl+Alt+Q')).toBe(true)
    expect(isValidHotkeyFormat('Ctrl+Shift+Win+F5')).toBe(true)
    expect(isValidHotkeyFormat('Q')).toBe(false)
    expect(isValidHotkeyFormat('Ctrl')).toBe(false)
    expect(isValidHotkeyFormat('Ctrl+Alt+Ctrl')).toBe(false)
  })
})

describe('hotkey save sequence (frozen contract)', () => {
  let i18n: Awaited<ReturnType<typeof setupTestEnv>>['i18n']

  beforeEach(async () => {
    const env = await setupFreshEnv()
    i18n = env.i18n
  })

  it('apply_hotkeys first, PUT settings only after it registered', async () => {
    const settings = useSettingsStore()
    await settings.load()
    const api = await getClient()
    const order: string[] = []
    const putSpy = vi.spyOn(api, 'updateSettings').mockImplementation(async () => {
      order.push('put-settings')
    })
    __setHotkeyInvokeForTests(async () => {
      order.push('apply-hotkeys')
      return { registered: true, conflict: null }
    })

    const wrapper = mount(GeneralSection, { global: { plugins: [i18n] } })
    const toggle = wrapper.find('[data-testid="hotkey-toggle"]')
    await toggle.trigger('focus')
    await toggle.trigger('keydown', { key: 'p', ctrlKey: true, altKey: true })
    await flushPromises()
    await flushPromises()

    expect(order).toEqual(['apply-hotkeys', 'put-settings'])
    expect(putSpy).toHaveBeenCalledTimes(1)
    const payload = putSpy.mock.calls[0][0] as Record<string, unknown>
    expect(payload.hotkey_toggle_window).toBe('Ctrl+Alt+P')
    expect(wrapper.find('[data-testid="hotkey-conflict"]').exists()).toBe(false)
  })

  it('conflict: no PUT, inline error with the offending combo, input reverted', async () => {
    const settings = useSettingsStore()
    await settings.load()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')

    const wrapper = mount(GeneralSection, { global: { plugins: [i18n] } })
    const toggle = wrapper.find('[data-testid="hotkey-toggle"]')

    // Record the OTHER hotkey's combo -> mock registration reports a conflict.
    await toggle.trigger('focus')
    await toggle.trigger('keydown', { key: 'q', ctrlKey: true, altKey: true })
    await flushPromises()
    await flushPromises()

    expect(putSpy).not.toHaveBeenCalled()
    const conflict = wrapper.find('[data-testid="hotkey-conflict"]')
    expect(conflict.exists()).toBe(true)
    expect(conflict.text()).toContain('Ctrl+Alt+Q')
    // The previous value is displayed again (reverted input).
    expect(toggle.text()).toContain('Ctrl+Alt+Space')
    expect(settings.app?.hotkey_toggle_window).toBe('Ctrl+Alt+Space')
  })

  it('forwards the frozen payload shape to the Tauri command', async () => {
    const calls: Array<{ cmd: string; args?: unknown }> = []
    __setHotkeyInvokeForTests(async (cmd, args) => {
      calls.push({ cmd, args })
      return { registered: true, conflict: null }
    })
    const result = await applyHotkeys({
      show_hide: 'Ctrl+Alt+Space',
      translate_selection: 'Ctrl+Alt+Q',
    })
    expect(calls).toEqual([
      {
        cmd: 'apply_hotkeys',
        args: { hotkeys: { show_hide: 'Ctrl+Alt+Space', translate_selection: 'Ctrl+Alt+Q' } },
      },
    ])
    expect(result).toEqual({ registered: true, conflict: null })
  })

  it('maps a host rejection to registered=false with the conflict combo', async () => {
    __setHotkeyInvokeForTests(async () => ({ registered: false, conflict: 'Ctrl+Alt+Q' }))
    const result = await applyHotkeys({
      show_hide: 'Ctrl+Alt+Q',
      translate_selection: 'Ctrl+Alt+Q',
    })
    expect(result.registered).toBe(false)
    expect(result.conflict).toBe('Ctrl+Alt+Q')
  })

  it('treats a failing host command as not registered (previous bindings stay)', async () => {
    __setHotkeyInvokeForTests(async () => {
      throw new Error('command missing')
    })
    const result = await applyHotkeys({
      show_hide: 'Ctrl+Alt+P',
      translate_selection: 'Ctrl+Alt+Q',
    })
    expect(result).toEqual({ registered: false, conflict: null })
  })

  it('mock registration conflicts when one hotkey equals the other', async () => {
    const conflict = await applyHotkeys({
      show_hide: 'Ctrl+Alt+Q',
      translate_selection: 'Ctrl+Alt+Q',
    })
    expect(conflict).toEqual({ registered: false, conflict: 'Ctrl+Alt+Q' })
    const ok = await applyHotkeys({
      show_hide: 'Ctrl+Alt+Space',
      translate_selection: 'Ctrl+Alt+Q',
    })
    expect(ok).toEqual({ registered: true, conflict: null })
  })

  it('boot registration sends the current settings values after load', async () => {
    const calls: Array<{ cmd: string; args?: unknown }> = []
    __setHotkeyInvokeForTests(async (cmd, args) => {
      calls.push({ cmd, args })
      return { registered: true, conflict: null }
    })
    const result = await registerHotkeysFromSettings()
    expect(result).toEqual({ registered: true, conflict: null })
    expect(calls).toEqual([
      {
        cmd: 'apply_hotkeys',
        args: { hotkeys: { show_hide: 'Ctrl+Alt+Space', translate_selection: 'Ctrl+Alt+Q' } },
      },
    ])
  })
})
