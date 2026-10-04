/**
 * Shared test bootstrap: fresh pinia + initialized mock API client + i18n.
 */
import { createPinia, setActivePinia } from 'pinia'
import { initApi, __resetApiClient } from '@/api'
import { i18n } from '@/i18n'

export async function setupTestEnv(): Promise<{ i18n: typeof import('@/i18n').i18n }> {
  setActivePinia(createPinia())
  await initApi()
  return { i18n }
}

/**
 * Same as setupTestEnv but with a brand-new mock client, so suites that
 * assert against cumulative backend state (history, tabs, settings) start
 * from a clean slate per test.
 */
export async function setupFreshEnv(): Promise<{ i18n: typeof import('@/i18n').i18n }> {
  __resetApiClient()
  return setupTestEnv()
}
