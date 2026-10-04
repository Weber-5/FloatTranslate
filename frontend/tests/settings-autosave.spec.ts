import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { setupTestEnv } from './helpers'
import { getClient } from '@/api'
import { useSettingsStore } from '@/stores/settings'

describe('settings auto-save (Phase 2: text fields debounced 800ms, toggles immediate)', () => {
  beforeEach(async () => {
    await setupTestEnv()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('coalesces rapid text-field changes into one PUT /settings after 800ms', async () => {
    vi.useFakeTimers()
    const settings = useSettingsStore()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')

    await settings.load()
    putSpy.mockClear()

    settings.saveAppDebounced({ context_tokens: 123 })
    await vi.advanceTimersByTimeAsync(300)
    settings.saveAppDebounced({ context_tokens: 456 })
    await vi.advanceTimersByTimeAsync(300)
    settings.saveAppDebounced({ ai_system_prompt: 'custom prompt' })

    // Still pending before the debounce window elapses.
    expect(putSpy).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(800)
    expect(putSpy).toHaveBeenCalledTimes(1)
    const payload = putSpy.mock.calls[0][0] as Record<string, unknown>
    expect(payload.context_tokens).toBe(456)
    expect(payload.ai_system_prompt).toBe('custom prompt')
    expect(settings.app?.context_tokens).toBe(456)
  })

  it('saveApp (toggles/selects) persists immediately', async () => {
    const settings = useSettingsStore()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')

    await settings.load()
    putSpy.mockClear()

    await settings.saveApp({ auto_compact: false })
    expect(putSpy).toHaveBeenCalledTimes(1)
    expect((putSpy.mock.calls[0][0] as Record<string, unknown>).auto_compact).toBe(false)
    expect(settings.app?.auto_compact).toBe(false)
  })

  it('flushAppSave persists pending changes without waiting for the debounce', async () => {
    vi.useFakeTimers()
    const settings = useSettingsStore()
    const api = await getClient()
    const putSpy = vi.spyOn(api, 'updateSettings')

    await settings.load()
    putSpy.mockClear()

    settings.saveAppDebounced({ output_tokens: 8192 })
    await settings.flushAppSave()
    expect(putSpy).toHaveBeenCalledTimes(1)
    expect(settings.app?.output_tokens).toBe(8192)
  })

  it('the clamp notice compares configured vs effective values from capabilities', async () => {
    const settings = useSettingsStore()
    await settings.load()

    // Mock capabilities clamp 1,000,000 → 128,000: the notice must appear and
    // report both values. With a real backend the same computation runs on the
    // returned configured/effective pair.
    const notice = settings.clampNotice
    expect(notice).not.toBeNull()
    expect(notice?.configured).toBe(1000000)
    expect(notice?.effective).toBe(128000)
  })
})
