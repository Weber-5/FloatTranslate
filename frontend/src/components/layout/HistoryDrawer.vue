<script setup lang="ts">
/**
 * Translation history drawer — an overlay opened from the title bar, not a
 * bottom-nav entry (docs/00 §2). List: kind, snippet, time. Actions: open
 * (new tab), single delete, clear all.
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { HistoryItem, HistoryQuery, TranslationKind } from '@/api/types'
import { useApi, toApiError } from '@/api'
import { useUiStore } from '@/stores/ui'
import { useTranslationStore } from '@/stores/translation'
import IconButton from '@/components/common/IconButton.vue'
import IconClose from '@/components/icons/IconClose.vue'
import IconSearch from '@/components/icons/IconSearch.vue'
import IconTrash from '@/components/icons/IconTrash.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import StateBanner from '@/components/common/StateBanner.vue'
import ConfirmModal from '@/components/common/ConfirmModal.vue'

const { t } = useI18n()
const ui = useUiStore()
const translationStore = useTranslationStore()

const items = ref<HistoryItem[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const query = ref('')
const kindFilter = ref<'all' | TranslationKind>('all')

const deleteTarget = ref<HistoryItem | null>(null)
const clearConfirmOpen = ref(false)

let searchTimer: ReturnType<typeof setTimeout> | null = null
let opened = false

const emptyResult = computed(
  () => !loading.value && !error.value && items.value.length === 0,
)

watch(
  () => ui.historyOpen,
  (open) => {
    if (open && !opened) {
      opened = true
      void load()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
})

function buildQuery(cursor?: string): HistoryQuery {
  return {
    query: query.value.trim() || undefined,
    kind: kindFilter.value === 'all' ? undefined : kindFilter.value,
    limit: 50,
    cursor,
  }
}

async function load(append = false): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const page = await useApi().listHistory(buildQuery(append ? (nextCursor.value ?? undefined) : undefined))
    items.value = append ? [...items.value, ...page.items] : page.items
    nextCursor.value = page.next_cursor ?? null
  } catch (err) {
    error.value = toApiError(err).message
  } finally {
    loading.value = false
  }
}

function onSearchInput(): void {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    void load()
  }, 250)
}

function setKindFilter(kind: 'all' | TranslationKind): void {
  kindFilter.value = kind
  void load()
}

function open(item: HistoryItem): void {
  translationStore.openHistoryItem(item)
  ui.historyOpen = false
}

function askDelete(item: HistoryItem): void {
  deleteTarget.value = item
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return
  try {
    await useApi().deleteHistoryItem(deleteTarget.value.id)
    await load()
  } catch (err) {
    error.value = toApiError(err).message
  } finally {
    deleteTarget.value = null
  }
}

async function confirmClear(): Promise<void> {
  try {
    await useApi().clearHistory()
    await load()
  } catch (err) {
    error.value = toApiError(err).message
  } finally {
    clearConfirmOpen.value = false
  }
}

function snippet(item: HistoryItem): string {
  const singleLine = item.input_text.replace(/\s+/g, ' ').trim()
  return singleLine.length > 60 ? `${singleLine.slice(0, 60)}…` : singleLine
}

function formatTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <Teleport to="body">
    <transition name="fade">
      <div v-if="ui.historyOpen" class="drawer-backdrop" @click.self="ui.historyOpen = false">
        <div class="drawer-panel" role="dialog" :aria-label="t('history.title')">
          <header class="drawer-header hairline-bottom">
            <h2 class="drawer-title">{{ t('history.title') }}</h2>
            <IconButton :label="t('common.close')" @click="ui.historyOpen = false">
              <IconClose :size="16" />
            </IconButton>
          </header>

          <div class="drawer-tools">
            <div class="search-box">
              <IconSearch :size="14" class="search-icon" />
              <input
                v-model="query"
                class="search-input"
                type="search"
                :placeholder="t('history.searchPlaceholder')"
                :aria-label="t('history.searchPlaceholder')"
                @input="onSearchInput"
              />
            </div>
            <div class="segmented" role="group" :aria-label="t('history.title')">
              <button
                v-for="option in ['all', 'word', 'text'] as const"
                :key="option"
                type="button"
                class="segment"
                :class="{ active: kindFilter === option }"
                @click="setKindFilter(option)"
              >
                {{ option === 'all' ? t('history.filterAll') : option === 'word' ? t('history.filterWord') : t('history.filterText') }}
              </button>
            </div>
          </div>

          <div class="drawer-body">
            <LoadingState v-if="loading" />
            <StateBanner
              v-else-if="error"
              variant="error"
              :title="t('errors.unknown')"
              :message="error"
              show-retry
              @retry="load()"
            />
            <EmptyState
              v-else-if="emptyResult"
              :title="t('history.emptyTitle')"
              :description="t('history.emptyDesc')"
            />
            <template v-else>
              <ul class="history-list">
                <li v-for="item in items" :key="item.id" class="history-row">
                  <button type="button" class="history-item" @click="open(item)">
                    <span class="history-top">
                      <span class="badge" :class="{ 'badge-accent': item.kind === 'word' }">
                        {{ item.kind === 'word' ? t('history.filterWord') : t('history.filterText') }}
                      </span>
                      <span class="history-time">{{ formatTime(item.created_at) }}</span>
                    </span>
                    <span class="history-snippet">{{ snippet(item) }}</span>
                  </button>
                  <IconButton
                    :label="t('history.deleteItem')"
                    tone="danger"
                    size="sm"
                    @click="askDelete(item)"
                  >
                    <IconTrash :size="14" />
                  </IconButton>
                </li>
              </ul>
              <button
                v-if="nextCursor"
                type="button"
                class="btn btn-ghost load-more"
                @click="load(true)"
              >
                {{ t('history.loadMore') }}
              </button>
            </template>
          </div>

          <footer class="drawer-footer hairline-top">
            <button
              type="button"
              class="btn btn-danger-ghost"
              :disabled="items.length === 0"
              @click="clearConfirmOpen = true"
            >
              {{ t('history.clearAll') }}
            </button>
          </footer>
        </div>
      </div>
    </transition>

    <ConfirmModal
      :open="deleteTarget !== null"
      :title="t('history.deleteItem')"
      :message="t('history.deleteMessage')"
      :confirm-label="t('common.delete')"
      @confirm="confirmDelete"
      @cancel="deleteTarget = null"
    />
    <ConfirmModal
      :open="clearConfirmOpen"
      :title="t('history.clearTitle')"
      :message="t('history.clearMessage')"
      :confirm-label="t('history.clearAll')"
      @confirm="confirmClear"
      @cancel="clearConfirmOpen = false"
    />
  </Teleport>
</template>

<style scoped>
.drawer-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.25);
  z-index: 50;
}

.drawer-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(340px, 92vw);
  display: flex;
  flex-direction: column;
  background: var(--bg-surface);
  border-left: 1px solid var(--hairline);
  box-shadow: var(--shadow-3);
  animation: drawer-in var(--transition-base) ease;
}

@keyframes drawer-in {
  from {
    transform: translateX(24px);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) var(--space-4);
}

.drawer-title {
  font-size: 15px;
  font-weight: 600;
}

.drawer-tools {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
}

.search-box {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-3);
  border: 1px solid var(--hairline-strong);
  border-radius: var(--radius-md);
  background: var(--bg-surface);
  color: var(--text-tertiary);
}

.search-box:focus-within {
  border-color: var(--accent);
}

.search-input {
  flex: 1;
  border: none;
  background: none;
  padding: 7px 0;
  min-width: 0;
}

.search-input:focus {
  outline: none;
}

.segmented {
  display: flex;
  gap: 2px;
  background: var(--bg-inset);
  border-radius: var(--radius-md);
  padding: 2px;
}

.segment {
  flex: 1;
  padding: 5px 0;
  border-radius: var(--radius-sm);
  font-size: 12px;
  color: var(--text-secondary);
  transition:
    background var(--transition-fast),
    color var(--transition-fast);
}

.segment.active {
  background: var(--bg-surface);
  color: var(--text-primary);
  box-shadow: var(--shadow-1);
}

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-2) var(--space-3) var(--space-4);
}

.history-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.history-row {
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.history-row:hover {
  background: var(--bg-hover);
  border-radius: var(--radius-md);
}

.history-item {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 3px;
  padding: var(--space-2) var(--space-2);
  text-align: left;
  border-radius: var(--radius-md);
}

.history-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: var(--space-2);
}

.history-time {
  font-size: 11px;
  color: var(--text-tertiary);
}

.history-snippet {
  font-size: 13px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.load-more {
  width: 100%;
  margin-top: var(--space-2);
}

.drawer-footer {
  padding: var(--space-3) var(--space-4);
  display: flex;
  justify-content: flex-end;
}
</style>
