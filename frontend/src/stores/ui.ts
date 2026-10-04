/**
 * Pure view state: overlay visibility. Theme lives in the settings store
 * (backend-persisted) and is applied by composables/useTheme.ts.
 */
import { ref } from 'vue'
import { defineStore } from 'pinia'

export const useUiStore = defineStore('ui', () => {
  const sidebarOpen = ref(false)
  const historyOpen = ref(false)

  function toggleSidebar(): void {
    sidebarOpen.value = !sidebarOpen.value
  }

  function toggleHistory(): void {
    historyOpen.value = !historyOpen.value
  }

  return { sidebarOpen, historyOpen, toggleSidebar, toggleHistory }
})
