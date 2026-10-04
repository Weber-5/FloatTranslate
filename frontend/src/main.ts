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
  // Resolve mock vs real client before any store touches the API.
  await initApi()

  const pinia = createPinia()
  const app = createApp(App)
  app.use(pinia)

  // Real mode: consume host health events; the boot probe starts after mount
  // so the full-screen "starting" gate is visible while the sidecar boots.
  const backend = useBackendStore(pinia)
  if (backend.isRealMode) {
    void backend.listenHostEvents()
  }

  app.use(i18n)
  app.use(router)
  try {
    await router.isReady()
  } catch {
    // Real mode with the sidecar still down: the initial navigation is
    // aborted by the backend gate and replayed once /health succeeds.
  }
  app.mount('#app')

  if (backend.isRealMode) {
    void backend.initialProbe()
  }
}

void bootstrap()
