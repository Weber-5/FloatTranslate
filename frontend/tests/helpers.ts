/**
 * Shared test bootstrap: fresh pinia + initialized mock API client + i18n.
 */
import { createPinia, setActivePinia } from 'pinia'
import { initApi } from '@/api'
import { i18n } from '@/i18n'

export async function setupTestEnv(): Promise<{ i18n: typeof import('@/i18n').i18n }> {
  setActivePinia(createPinia())
  await initApi()
  return { i18n }
}
