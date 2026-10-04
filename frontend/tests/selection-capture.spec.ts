import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { setupFreshEnv } from './helpers'
import {
  handleSelectionCaptured,
  resetSelectionGuard,
  SELECTION_DUPLICATE_GUARD_MS,
} from '@/services/selection'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import { registerHotkeysFromSettings, __setHotkeyInvokeForTests } from '@/services/hotkeys'

beforeEach(async () => {
  await setupFreshEnv()
  resetSelectionGuard()
})

afterEach(() => {
  __setHotkeyInvokeForTests(null)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('selection-captured handling (US-04)', () => {
  it('opens a NEW text tab and auto-sends the translation', async () => {
    const tabs = useTabsStore()
    const translation = useTranslationStore()
    await tabs.restore()
    expect(tabs.tabs).toHaveLength(1)

    const tabId = handleSelectionCaptured('The rabbit is running quickly.')
    expect(tabId).not.toBeNull()
    expect(tabs.tabs).toHaveLength(2)
    expect(tabs.activeId).toBe(tabId)

    const tab = tabs.tabs.find((entry) => entry.id === tabId)
    expect(tab?.kind).toBe('text')
    // Text tab title: first ~12 chars of the input + ellipsis.
    expect(tab?.title).toContain('The rabbit')

    const state = translation.stateFor(tabId as string)
    expect(state.input).toBe('The rabbit is running quickly.')
    // Auto-send: the state machine went straight to translating, then success.
    await vi.waitFor(() => {
      expect(state.status === 'success' || state.status === 'cache_success').toBe(true)
    })
    expect(state.response?.result).toHaveProperty('segments')
  })

  it('ignores empty and whitespace-only payloads', async () => {
    const tabs = useTabsStore()
    await tabs.restore()
    expect(handleSelectionCaptured('   ')).toBeNull()
    expect(handleSelectionCaptured(undefined)).toBeNull()
    expect(handleSelectionCaptured({ text: 'x' })).toBeNull()
    expect(tabs.tabs).toHaveLength(1)
  })

  it('skips an identical capture within the duplicate guard window', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z').getTime())

    const tabs = useTabsStore()
    await tabs.restore()
    expect(tabs.tabs).toHaveLength(1)

    const first = handleSelectionCaptured('Hello selection world')
    expect(first).not.toBeNull()
    expect(tabs.tabs).toHaveLength(2)

    vi.setSystemTime(new Date('2026-01-01T00:00:00Z').getTime() + 500)
    const duplicate = handleSelectionCaptured('Hello selection world')
    expect(duplicate).toBeNull()
    expect(tabs.tabs).toHaveLength(2)
  })

  it('allows the same text again after the guard window elapsed', async () => {
    vi.useFakeTimers()
    const start = new Date('2026-01-01T00:00:00Z').getTime()
    vi.setSystemTime(start)

    const tabs = useTabsStore()
    await tabs.restore()

    expect(handleSelectionCaptured('Hello selection world')).not.toBeNull()
    vi.setSystemTime(start + SELECTION_DUPLICATE_GUARD_MS + 10)
    expect(handleSelectionCaptured('Hello selection world')).not.toBeNull()
    expect(tabs.tabs).toHaveLength(3) // empty restore tab + two selection tabs
  })
})

describe('real-mode boot hotkey registration', () => {
  it('registers the stored pair once settings are loaded', async () => {
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
