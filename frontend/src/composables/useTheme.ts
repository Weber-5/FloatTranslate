/**
 * Applies the theme (System / Light / Dark) from the settings store to
 * `<html data-theme>`. When System, follows `prefers-color-scheme` live.
 */
import { computed, watch, onBeforeUnmount } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import type { ThemeMode } from '@/api/types'

let mediaQuery: MediaQueryList | null = null
let mediaListener: ((event: MediaQueryListEvent) => void) | null = null

function applyTheme(mode: ThemeMode): void {
  const resolved =
    mode === 'system'
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : mode
  document.documentElement.dataset.theme = resolved
}

export function useTheme() {
  const settings = useSettingsStore()
  const theme = computed<ThemeMode>(() => settings.app?.theme ?? 'system')

  watch(
    theme,
    (mode) => {
      applyTheme(mode)
      if (mode === 'system' && !mediaQuery) {
        mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
        mediaListener = () => applyTheme('system')
        mediaQuery.addEventListener('change', mediaListener)
      }
    },
    { immediate: true },
  )

  onBeforeUnmount(() => {
    if (mediaQuery && mediaListener) {
      mediaQuery.removeEventListener('change', mediaListener)
      mediaQuery = null
      mediaListener = null
    }
  })
}
