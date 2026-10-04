/**
 * Pure view state: overlay visibility, toasts, mobile tab bar. Theme lives
 * in the settings store (backend-persisted) and is applied by
 * composables/useTheme.ts.
 */
import { ref } from 'vue'
import { defineStore } from 'pinia'

export type ToastTone = 'info' | 'success' | 'error'

export interface Toast {
  id: number
  tone: ToastTone
  message: string
}

export const useUiStore = defineStore('ui', () => {
  const sidebarOpen = ref(false)
  const historyOpen = ref(false)

  const toasts = ref<Toast[]>([])
  let toastCounter = 0

  /**
   * Inline toast for business notices (docs/03 §11: never a blocking OS
   * modal). Auto-dismisses after 3s.
   */
  function showToast(message: string, tone: ToastTone = 'info'): void {
    toastCounter += 1
    const toast: Toast = { id: toastCounter, tone, message }
    toasts.value = [...toasts.value, toast]
    window.setTimeout(() => dismissToast(toast.id), 3000)
  }

  function dismissToast(id: number): void {
    toasts.value = toasts.value.filter((toast) => toast.id !== id)
  }

  function toggleSidebar(): void {
    sidebarOpen.value = !sidebarOpen.value
  }

  function toggleHistory(): void {
    historyOpen.value = !historyOpen.value
  }

  return {
    sidebarOpen,
    historyOpen,
    toasts,
    showToast,
    dismissToast,
    toggleSidebar,
    toggleHistory,
  }
})
