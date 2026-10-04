<script setup lang="ts">
/**
 * Browser-like tab bar: horizontal scroll/compress, drag reorder,
 * per-tab close, "+" new tab (docs/03 §6). Tabs are word/text only.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTabsStore } from '@/stores/tabs'
import IconButton from '@/components/common/IconButton.vue'
import IconClose from '@/components/icons/IconClose.vue'
import IconPlus from '@/components/icons/IconPlus.vue'

const { t } = useI18n()
const tabsStore = useTabsStore()

const dragIndex = ref<number | null>(null)

function onDragStart(index: number): void {
  dragIndex.value = index
}

function onDrop(index: number): void {
  if (dragIndex.value != null && dragIndex.value !== index) {
    tabsStore.moveTab(dragIndex.value, index)
  }
  dragIndex.value = null
}

function onDragEnd(): void {
  dragIndex.value = null
}
</script>

<template>
  <div class="tabbar" role="tablist" :aria-label="t('tabs.openNew')">
    <div
      v-for="(tab, index) in tabsStore.tabs"
      :key="tab.id"
      class="tab"
      :class="{ active: tab.id === tabsStore.activeId, dragging: dragIndex === index }"
      role="tab"
      :aria-selected="tab.id === tabsStore.activeId"
      :tabindex="0"
      draggable="true"
      data-testid="tab-item"
      @click="tabsStore.activate(tab.id)"
      @keydown.enter="tabsStore.activate(tab.id)"
      @dragstart="onDragStart(index)"
      @dragover.prevent
      @drop="onDrop(index)"
      @dragend="onDragEnd"
    >
      <span class="tab-kind" :class="`tab-kind-${tab.kind}`" aria-hidden="true">
        {{ tab.kind === 'word' ? t('tabs.kindWord') : t('tabs.kindText') }}
      </span>
      <span class="tab-title">{{ tab.title }}</span>
      <IconButton
        :label="`${t('tabs.close')}：${tab.title}`"
        size="sm"
        class="tab-close"
        @click.stop="tabsStore.close(tab.id)"
      >
        <IconClose :size="12" />
      </IconButton>
    </div>
    <IconButton :label="t('tabs.openNew')" class="tab-new" @click="tabsStore.newTab()">
      <IconPlus :size="16" />
    </IconButton>
  </div>
</template>

<style scoped>
.tabbar {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
  padding: 2px 0;
}

.tabbar::-webkit-scrollbar {
  display: none;
}

.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 110px;
  min-width: 74px;
  max-width: 170px;
  height: 30px;
  padding: 0 6px 0 10px;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  user-select: none;
  transition:
    background var(--transition-fast),
    color var(--transition-fast);
}

.tab:hover {
  background: var(--bg-hover);
}

.tab.active {
  background: var(--bg-surface);
  color: var(--text-primary);
  box-shadow: var(--shadow-1);
}

.tab.dragging {
  opacity: 0.5;
}

.tab-kind {
  flex: none;
  font-size: 10px;
  font-weight: 600;
  line-height: 1;
  padding: 2px 4px;
  border-radius: 4px;
}

.tab-kind-word {
  background: var(--accent-soft);
  color: var(--accent);
}

.tab-kind-text {
  background: var(--bg-inset);
  color: var(--text-secondary);
}

.tab-title {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tab-close {
  opacity: 0;
  transition: opacity var(--transition-fast);
}

.tab:hover .tab-close,
.tab.active .tab-close,
.tab:focus-within .tab-close {
  opacity: 1;
}

.tab-new {
  flex: none;
}
</style>
