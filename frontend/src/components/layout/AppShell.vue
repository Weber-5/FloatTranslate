<script setup lang="ts">
/**
 * App shell: title bar + body (router-view) + bottom nav, with the AI
 * sidebar as a right panel, the history drawer as an overlay and the global
 * toast host for inline business notices (docs/03 §11).
 * Restores persisted tabs on mount and hydrates them from stored
 * translations; flushes tab persistence on unload.
 */
import { onBeforeUnmount, onMounted } from 'vue'
import TitleBar from './TitleBar.vue'
import BottomNav from './BottomNav.vue'
import AiSidebar from './AiSidebar.vue'
import HistoryDrawer from './HistoryDrawer.vue'
import { useUiStore } from '@/stores/ui'
import { useTranslationStore } from '@/stores/translation'
import { handleTabShortcut } from '@/composables/useTabShortcuts'
import { useDebouncedTabsPersist } from '@/composables/useDebouncedTabsPersist'

const ui = useUiStore()
const translationStore = useTranslationStore()
const { flushPersist } = useDebouncedTabsPersist()

function onBeforeUnload(): void {
  void flushPersist()
}

/** Ctrl+T / Ctrl+W tab management (tooltip on the tab bar documents it). */
function onKeyDown(event: KeyboardEvent): void {
  if (handleTabShortcut(event)) event.preventDefault()
}

onMounted(() => {
  window.addEventListener('beforeunload', onBeforeUnload)
  window.addEventListener('keydown', onKeyDown)
  void translationStore.restoreTabsWithHydration()
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
  window.removeEventListener('keydown', onKeyDown)
})
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
    <div class="toast-host" aria-live="polite">
      <TransitionGroup name="toast">
        <div
          v-for="toast in ui.toasts"
          :key="toast.id"
          class="toast"
          :class="`toast-${toast.tone}`"
          data-testid="toast"
          role="status"
          @click="ui.dismissToast(toast.id)"
        >
          {{ toast.message }}
        </div>
      </TransitionGroup>
    </div>
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

.toast-host {
  position: fixed;
  bottom: 56px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  z-index: 80;
  pointer-events: none;
}

.toast {
  pointer-events: auto;
  max-width: min(420px, 86vw);
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-lg);
  font-size: 13px;
  background: var(--bg-surface);
  border: 1px solid var(--hairline);
  box-shadow: var(--shadow-2);
  color: var(--text-primary);
  cursor: pointer;
  word-break: break-word;
}

.toast-success {
  border-color: var(--success);
  color: var(--success);
}

.toast-error {
  border-color: var(--danger);
  color: var(--danger);
}

.toast-enter-active,
.toast-leave-active {
  transition:
    opacity var(--transition-fast),
    transform var(--transition-fast);
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(6px);
}
</style>
