/**
 * Selection capture handling (docs/07 §5, US-04): the Rust host copies the
 * selection, wakes the window and emits `selection-captured` with payload
 * { text: string }. The frontend opens a NEW text tab with the text as input
 * and auto-sends the translation. Empty/whitespace payloads are ignored; the
 * same text captured twice within 1500ms is treated as a hotkey bounce and
 * skipped once.
 */
import { isMockMode } from '@/api'
import { useTranslationStore } from '@/stores/translation'
import { showTranslatePage } from './navigation'

export const SELECTION_CAPTURED_EVENT = 'selection-captured'
export const SELECTION_DUPLICATE_GUARD_MS = 1500

let lastCaptured: { text: string; at: number } = { text: '', at: 0 }

/** Test-only: forget the last captured selection. */
export function resetSelectionGuard(): void {
  lastCaptured = { text: '', at: 0 }
}

/**
 * Handle one captured selection; returns the new tab id, or null when the
 * payload was ignored (empty, non-string or a duplicate within the guard
 * window).
 */
export function handleSelectionCaptured(payload: unknown): string | null {
  const text = typeof payload === 'string' ? payload : undefined
  if (text === undefined) return null
  const trimmed = text.trim()
  if (trimmed.length === 0) return null

  const now = Date.now()
  if (
    trimmed === lastCaptured.text &&
    now - lastCaptured.at < SELECTION_DUPLICATE_GUARD_MS
  ) {
    return null
  }
  lastCaptured = { text: trimmed, at: now }

  const tabId = useTranslationStore().openSelectionTab(trimmed)
  // improvement bug #6: the capture can happen on any page (设置/单词本 or
  // the blank pre-navigation state); jump to the translate page so the new
  // tab is actually visible instead of requiring manual clicks.
  showTranslatePage()
  return tabId
}

/** Listen to the host event in real mode; mock mode is a no-op. */
export async function listenSelectionCaptured(): Promise<void> {
  if (isMockMode()) return
  try {
    const { listen } = await import('@tauri-apps/api/event')
    await listen<{ text?: unknown }>(SELECTION_CAPTURED_EVENT, (event) => {
      handleSelectionCaptured(event.payload?.text)
    })
  } catch {
    // Older host bundle without the event bridge: selection capture stays
    // inactive until the host emits through Tauri.
  }
}
