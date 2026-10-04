<script setup lang="ts">
/**
 * App shell: title bar + body (router-view) + bottom nav, with the AI
 * sidebar as a right panel and the history drawer as an overlay.
 * Restores persisted tabs on mount and hydrates them from stored
 * translations; flushes tab persistence on unload.
 */
import { onBeforeUnmount, onMounted } from 'vue'
import TitleBar from './TitleBar.vue'
import BottomNav from './BottomNav.vue'
import AiSidebar from './AiSidebar.vue'
import HistoryDrawer from './HistoryDrawer.vue'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import { useUiStore } from '@/stores/ui'
import { useApi } from '@/api'
import { useDebouncedTabsPersist } from '@/composables/useDebouncedTabsPersist'

const ui = useUiStore()
const tabsStore = useTabsStore()
const { flushPersist } = useDebouncedTabsPersist()

function onBeforeUnload(): void {
  void flushPersist()
}

onMounted(() => {
  window.addEventListener('beforeunload', onBeforeUnload)
  void restoreTabs()
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
})

async function restoreTabs(): Promise<void> {
  try {
    await tabsStore.restore()
  } catch {
    tabsStore.ensureTab()
    return
  }
  const translationStore = useTranslationStore()
  await Promise.all(
    tabsStore.tabs.map(async (tab) => {
      const translationId = tab.payload.translation_id
      if (!translationId || tab.payload.word_data || translationStore.stateFor(tab.id).response) {
        return
      }
      try {
        const response = await useApi().getTranslation(translationId)
        translationStore.hydrateFromResponse(tab.id, response)
      } catch {
        // Stored translation no longer available — the tab stays an input tab.
      }
    }),
  )
}
</script>

<template>
  <div class="app-shell" :class="{ 'sidebar-open': ui.sidebarOpen }">
    <div class="app-main">
      <TitleBar />
      <main class="app-body">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
      <BottomNav />
    </div>
    <AiSidebar v-if="ui.sidebarOpen" class="app-sidebar" />
    <HistoryDrawer />
  </div>
</template>

<style scoped>
.app-shell {
  display: flex;
  height: 100%;
  overflow: hidden;
}

.app-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.app-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  background: var(--bg-app);
}

.app-sidebar {
  width: min(420px, 100%);
  flex: none;
  border-left: 1px solid var(--hairline);
  background: var(--bg-surface-2);
  animation: slide-in var(--transition-base) ease;
}

@keyframes slide-in {
  from {
    transform: translateX(16px);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}
</style>
