/**
 * improvement bug #2 regression guard.
 *
 * The packaged app called `win.currentMonitor()`, but `@tauri-apps/api@2.12`
 * exposes `currentMonitor` as a MODULE function — the resulting TypeError was
 * swallowed by an empty catch, so the sidebar opened inside the unchanged
 * 420px window instead of widening it. These tests pin the behaviour down and
 * fail if the resize ever becomes dependant on the optional monitor lookup.
 */
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import {
  expandForSidebar,
  collapseSidebar,
  restoreMainWidthAtBoot,
  SIDEBAR_WIDTH,
} from '@/services/window'

interface Size {
  width: number
  height: number
}
interface Point {
  x: number
  y: number
}
interface MonitorInfo {
  position: Point
  size: Size
  scaleFactor: number
}

const h = vi.hoisted(() => {
  const state = {
    /** Inner size: the space `setSize` writes to. */
    inner: 420,
    height: 720,
    /** Invisible Win11 resize border between inner and outer size. */
    border: 16,
    scale: 1,
    position: { x: 0, y: 0 },
    monitor: null as MonitorInfo | null,
    monitorThrows: false,
  }
  state.monitor = {
    position: { x: 0, y: 0 },
    size: { width: 2560, height: 1440 },
    scaleFactor: 1,
  }
  const setSize = vi.fn(async (size: unknown) => {
    const s = size as Size
    state.inner = s.width
    state.height = s.height
  })
  const setPosition = vi.fn(async (position: unknown) => {
    state.position = position as Point
  })
  return { state, setSize, setPosition }
})

vi.mock('@tauri-apps/api/window', () => ({
  getCurrentWindow: () => ({
    innerSize: async () => ({ width: h.state.inner, height: h.state.height }),
    outerSize: async () => ({
      width: h.state.inner + h.state.border,
      height: h.state.height + h.state.border,
    }),
    scaleFactor: async () => h.state.scale,
    outerPosition: async () => h.state.position,
    setSize: h.setSize,
    setPosition: h.setPosition,
  }),
  // Module-level function (NOT a method on the window object).
  currentMonitor: async () => {
    if (h.state.monitorThrows) throw new Error('monitor api unavailable')
    return h.state.monitor
  },
}))

vi.mock('@tauri-apps/api/dpi', () => ({
  LogicalSize: class {
    constructor(
      public width: number,
      public height: number,
    ) {}
  },
  LogicalPosition: class {
    constructor(
      public x: number,
      public y: number,
    ) {}
  },
}))

const internals = window as unknown as { __TAURI_INTERNALS__?: unknown }

function lastSize(): Size {
  return h.setSize.mock.calls[h.setSize.mock.calls.length - 1][0] as unknown as Size
}

beforeEach(() => {
  internals.__TAURI_INTERNALS__ = {}
  h.setSize.mockClear()
  h.setPosition.mockClear()
  h.state.inner = 420
  h.state.height = 720
  h.state.position = { x: 0, y: 0 }
  h.state.monitorThrows = false
  h.state.monitor = {
    position: { x: 0, y: 0 },
    size: { width: 2560, height: 1440 },
    scaleFactor: 1,
  }
  window.localStorage.clear()
})

afterEach(() => {
  delete internals.__TAURI_INTERNALS__
})

describe('sidebar fan-out (improvement bug #2)', () => {
  it('widens the window by the sidebar width and keeps the main width', async () => {
    await expandForSidebar()

    expect(h.setSize).toHaveBeenCalledTimes(1)
    expect(lastSize().width).toBe(420 + SIDEBAR_WIDTH)
    expect(lastSize().height).toBe(720)
    // The pre-expansion width IS the width the translate pane must keep.
    expect(window.localStorage.getItem('ft.main-width')).toBe('420')
  })

  it('still resizes when the monitor API is unavailable (the shipped bug)', async () => {
    h.state.monitorThrows = true
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    await expandForSidebar()

    expect(h.setSize).toHaveBeenCalledTimes(1)
    expect(lastSize().width).toBe(420 + SIDEBAR_WIDTH)
    // The failure is reported, never silently ignored.
    expect(errorSpy).toHaveBeenCalled()
    errorSpy.mockRestore()
  })

  it('shifts left instead of overflowing the monitor right edge', async () => {
    h.state.position = { x: 2200, y: 40 } // 2200 + 840 > 2560

    await expandForSidebar()

    expect(h.setPosition).toHaveBeenCalledTimes(1)
    const pos = h.setPosition.mock.calls[0][0] as unknown as Point
    expect(pos.x).toBe(2200 - (2200 + 840 - 2560))
    expect(pos.y).toBe(40)
    expect(lastSize().width).toBe(840)
  })

  it('round-trips open → close back to the exact original width (no drift)', async () => {
    await expandForSidebar()
    expect(h.state.inner).toBe(420 + SIDEBAR_WIDTH)
    h.setSize.mockClear()

    await collapseSidebar()

    expect(h.setSize).toHaveBeenCalledTimes(1)
    expect(lastSize().width).toBe(420)
    expect(h.state.inner).toBe(420)
  })

  it('does not widen a second time when expand runs twice', async () => {
    await expandForSidebar()
    // window is now widened; a second expand (state desync) must be a no-op
    h.setSize.mockClear()

    await expandForSidebar()

    expect(window.localStorage.getItem('ft.main-width')).toBe('420')
    expect(lastSize().width).toBe(420 + SIDEBAR_WIDTH)
    expect(h.state.inner).toBe(420 + SIDEBAR_WIDTH)
  })

  it('boot guard shrinks only the widened geometry', async () => {
    await expandForSidebar()
    h.setSize.mockClear()

    // Exited with the sidebar open: persisted geometry is the widened one.
    await restoreMainWidthAtBoot()
    expect(lastSize().width).toBe(420)

    // A deliberately wider window must be left alone.
    h.setSize.mockClear()
    h.state.inner = 620
    await restoreMainWidthAtBoot()
    expect(h.setSize).not.toHaveBeenCalled()
  })
})
