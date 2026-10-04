/**
 * Debounced tabs persistence (frozen contract: getTabs → putTabs debounced
 * 500ms). The 500ms debounce lives in the tabs store; this composable is the
 * component-facing wrapper. Use flushPersist on beforeunload.
 */
import { useTabsStore } from '@/stores/tabs'

export function useDebouncedTabsPersist() {
  const tabs = useTabsStore()
  return {
    schedulePersist: tabs.schedulePersist,
    flushPersist: (): Promise<void> => tabs.persistNow(),
  }
}
