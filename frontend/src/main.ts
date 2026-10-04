import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import { initApi } from './api'
import { useBackendStore } from './stores/backend'
import './styles/tokens.css'
import './styles/base.css'

async function bootstrap(): Promise<void> {
  const pinia = createPinia()
  const app = createApp(App)
  app.use(pinia)

  const backend = useBackendStore(pinia)
  app.use(i18n)
  app.use(router)
  // Mount FIRST: in real mode the full-screen gate renders while the sidecar
  // boots (the webview usually wins the race against sidecar READY, so any
  // await before mount would leave a blank window). The router guard blocks
  // navigation (and API calls) until /health answers.
  app.mount('#app')

  if (backend.isRealMode) {
    void backend.listenHostEvents()
    void bootstrapReal(backend)
  } else {
    void backend.initialProbe()
  }
}

/** Real mode: resolve the client (retries until READY), then run the probe. */
async function bootstrapReal(backend: ReturnType<typeof useBackendStore>): Promise<void> {
  try {
    await initApi()
  } catch {
    // The real client retries internally and never rejects; kept defensive.
    return
  }
  await backend.initialProbe()
}

void bootstrap()
