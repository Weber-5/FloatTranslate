import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import { initApi } from './api'
import './styles/tokens.css'
import './styles/base.css'

async function bootstrap(): Promise<void> {
  // Resolve mock vs real client before any store touches the API.
  await initApi()

  const app = createApp(App)
  app.use(createPinia())
  app.use(i18n)
  app.use(router)
  await router.isReady()
  app.mount('#app')
}

void bootstrap()
