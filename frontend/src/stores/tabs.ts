/**
 * Browser-like tab store (docs/00 §2 标签页):
 * word/text tabs, unlimited count, close without deleting history,
 * restore after restart (getTabs → debounced putTabs, 500ms).
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type {
  HistoryItem,
  TabPayload,
  TabState,
  TranslationKind,
  TranslationResponse,
  WordTranslation,
} from '@/api/types'
import { useApi } from '@/api'
import { i18n } from '@/i18n'

export interface UiTab {
  id: string
  kind: TranslationKind
  title: string
  payload: TabPayload
}

const PERSIST_DEBOUNCE_MS = 500

let tabCounter = 0
function createTabId(): string {
  tabCounter += 1
  return `tab-${Date.now().toString(36)}-${tabCounter}`
}

function textSnippet(text: string, max = 24): string {
  const singleLine = text.replace(/\s+/g, ' ').trim()
  return singleLine.length > max ? `${singleLine.slice(0, max)}…` : singleLine
}

function isEmptyPayload(payload: TabPayload): boolean {
  return (
    payload.translation_id == null && payload.word == null && payload.text == null && payload.word_data == null
  )
}

export const useTabsStore = defineStore('tabs', () => {
  const tabs = ref<UiTab[]>([])
  const activeId = ref<string | null>(null)
  const restored = ref(false)

  const activeTab = computed<UiTab | null>(
    () => tabs.value.find((tab) => tab.id === activeId.value) ?? null,
  )

  let persistTimer: ReturnType<typeof setTimeout> | null = null

  function toTabStates(): TabState[] {
    return tabs.value.map((tab, index) => ({
      id: tab.id,
      kind: tab.kind,
      title: tab.title,
      position: index,
      is_active: tab.id === activeId.value,
      payload: { ...tab.payload },
    }))
  }

  function schedulePersist(): void {
    if (persistTimer) clearTimeout(persistTimer)
    persistTimer = setTimeout(() => {
      void persistNow()
    }, PERSIST_DEBOUNCE_MS)
  }

  async function persistNow(): Promise<void> {
    if (persistTimer) {
      clearTimeout(persistTimer)
      persistTimer = null
    }
    await useApi().putTabs(toTabStates())
  }

  function pushTab(tab: UiTab): UiTab {
    tabs.value.push(tab)
    activeId.value = tab.id
    schedulePersist()
    return tab
  }

  function newEmptyTab(): UiTab {
    return {
      id: createTabId(),
      kind: 'text',
      title: i18n.global.t('tabs.newTab'),
      payload: {},
    }
  }

  /** Guarantee at least one tab exists (e.g. after closing the last one). */
  function ensureTab(): UiTab {
    if (tabs.value.length === 0) {
      const tab = newEmptyTab()
      tabs.value.push(tab)
      activeId.value = tab.id
    }
    return tabs.value[0]
  }

  /** "+" action: focus the current empty tab instead of stacking empties. */
  function newTab(): UiTab {
    const current = activeTab.value
    if (current && isEmptyPayload(current.payload)) return current
    return pushTab(newEmptyTab())
  }

  function openWordTab(
    word: string,
    opts?: { wordData?: WordTranslation; translationId?: string },
  ): UiTab {
    const payload: TabPayload = { word }
    if (opts?.wordData) payload.word_data = opts.wordData
    if (opts?.translationId) payload.translation_id = opts.translationId
    return pushTab({
      id: createTabId(),
      kind: 'word',
      title: word.trim(),
      payload,
    })
  }

  function openTextTab(text: string, translationId?: string): UiTab {
    const payload: TabPayload = { text }
    if (translationId) payload.translation_id = translationId
    return pushTab({
      id: createTabId(),
      kind: 'text',
      title: textSnippet(text),
      payload,
    })
  }

  function openFromHistory(item: HistoryItem): UiTab {
    const payload: TabPayload = { translation_id: item.id }
    if (item.kind === 'word') payload.word = item.input_text
    else payload.text = item.input_text
    return pushTab({
      id: createTabId(),
      kind: item.kind,
      title: item.kind === 'word' ? item.input_text : textSnippet(item.input_text),
      payload,
    })
  }

  function applyTranslationResult(tabId: string, response: TranslationResponse): void {
    const tab = tabs.value.find((entry) => entry.id === tabId)
    if (!tab) return
    tab.kind = response.kind
    const result = response.result
    if (response.kind === 'word' && 'word' in result) {
      tab.title = result.word
      tab.payload.word = result.word
    } else if ('source_markdown' in result) {
      const input = tab.payload.text ?? textSnippet(result.source_markdown, 40)
      tab.payload.text = input
      tab.title = textSnippet(input)
    }
    tab.payload.translation_id = response.translation_id
    schedulePersist()
  }

  function close(id: string): void {
    const index = tabs.value.findIndex((tab) => tab.id === id)
    if (index === -1) return
    tabs.value.splice(index, 1)
    if (activeId.value === id) {
      const neighbor = tabs.value[index - 1] ?? tabs.value[index] ?? null
      activeId.value = neighbor?.id ?? null
    }
    ensureTab()
    schedulePersist()
  }

  function activate(id: string): void {
    if (tabs.value.some((tab) => tab.id === id)) {
      activeId.value = id
      schedulePersist()
    }
  }

  /** Reorder (TabBar drag & drop); normalizes positions and persists. */
  function moveTab(from: number, to: number): void {
    if (from === to) return
    if (from < 0 || from >= tabs.value.length) return
    const target = Math.max(0, Math.min(tabs.value.length - 1, to))
    const [moved] = tabs.value.splice(from, 1)
    tabs.value.splice(target, 0, moved)
    schedulePersist()
  }

  /** Restore the last session's tabs (docs/00 §2: 重启恢复). */
  async function restore(): Promise<void> {
    const remote = await useApi().getTabs()
    const sorted = [...remote].sort((a, b) => a.position - b.position)
    tabs.value = sorted.map((state) => ({
      id: state.id,
      kind: state.kind,
      title: state.title,
      payload: { ...state.payload },
    }))
    const active = sorted.find((state) => state.is_active)
    activeId.value = active?.id ?? tabs.value[0]?.id ?? null
    ensureTab()
    restored.value = true
  }

  return {
    tabs,
    activeId,
    restored,
    activeTab,
    newTab,
    ensureTab,
    openWordTab,
    openTextTab,
    openFromHistory,
    applyTranslationResult,
    close,
    activate,
    moveTab,
    persistNow,
    schedulePersist,
    restore,
  }
})
