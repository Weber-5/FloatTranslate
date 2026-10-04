<script setup lang="ts">
/**
 * Self-drawn title bar (no native titlebar, docs/00 §2): draggable region,
 * browser-like tabs row, history + AI sidebar toggles, window actions.
 * Close hides to tray; it never quits the app.
 */
import { useI18n } from 'vue-i18n'
import TabBar from './TabBar.vue'
import IconButton from '@/components/common/IconButton.vue'
import IconHistory from '@/components/icons/IconHistory.vue'
import IconAi from '@/components/icons/IconAi.vue'
import IconMinus from '@/components/icons/IconMinus.vue'
import IconClose from '@/components/icons/IconClose.vue'
import { useUiStore } from '@/stores/ui'
import { minimizeWindow, hideWindowToTray } from '@/services/window'

const { t } = useI18n()
const ui = useUiStore()
</script>

<template>
  <header class="titlebar" data-tauri-drag-region>
    <TabBar class="titlebar-tabs" />
    <div class="titlebar-actions">
      <IconButton
        :label="t('titlebar.history')"
        :tone="ui.historyOpen ? 'accent' : 'default'"
        @click="ui.toggleHistory()"
      >
        <IconHistory :size="16" />
      </IconButton>
      <IconButton
        :label="t('titlebar.aiSidebar')"
        :tone="ui.sidebarOpen ? 'accent' : 'default'"
        @click="ui.toggleSidebar()"
      >
        <IconAi :size="16" />
      </IconButton>
      <span class="titlebar-separator" aria-hidden="true" />
      <IconButton :label="t('titlebar.minimize')" @click="minimizeWindow()">
        <IconMinus :size="16" />
      </IconButton>
      <IconButton
        :label="t('titlebar.close')"
        tone="danger"
        class="titlebar-close"
        @click="hideWindowToTray()"
      >
        <IconClose :size="16" />
      </IconButton>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 44px;
  padding: 0 var(--space-2) 0 var(--space-3);
  background: var(--bg-app);
  border-bottom: 1px solid var(--hairline);
  flex: none;
}

.titlebar-tabs {
  padding-left: 2px;
}

.titlebar-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: none;
  -webkit-app-region: no-drag;
}

.titlebar-separator {
  width: 1px;
  height: 16px;
  background: var(--hairline);
  margin: 0 4px;
}

.titlebar-close:hover:not(:disabled) {
  background: var(--danger);
  color: #fff;
}
</style>
