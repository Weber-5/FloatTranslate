/**
 * In-app tab keyboard shortcuts (Phase 3): Ctrl+T opens a tab, Ctrl+W closes
 * the active one. The tabs store guarantees at least one tab always remains
 * (closing the last one creates a fresh empty text tab). Modifiers must be
 * Ctrl only — Ctrl+Shift/Ctrl+Alt variants and plain T/W are ignored.
 */
import { useTabsStore } from '@/stores/tabs'

/** Core handler (test-friendly): returns true when the key was handled. */
export function handleTabShortcut(event: KeyboardEvent): boolean {
  if (!event.ctrlKey || event.altKey || event.shiftKey || event.metaKey) return false
  const key = event.key.toLowerCase()
  const tabs = useTabsStore()
  if (key === 't') {
    tabs.newTab()
    return true
  }
  if (key === 'w') {
    if (tabs.activeId !== null) tabs.close(tabs.activeId)
    return true
  }
  return false
}
