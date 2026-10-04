<script setup lang="ts">
/**
 * AI Sidebar (docs/03 §9): session switcher, messages area with collapsible
 * reasoning, composer with Enter send / Shift+Enter newline, thinking toggle,
 * slash command picker (/compact /clear /context /export), send / stop.
 * Phase 1 uses the mock generation stream (display logic only).
 */
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatsStore } from '@/stores/chats'
import { useUiStore } from '@/stores/ui'
import IconButton from '@/components/common/IconButton.vue'
import IconChevronDown from '@/components/icons/IconChevronDown.vue'
import IconClose from '@/components/icons/IconClose.vue'
import IconEdit from '@/components/icons/IconEdit.vue'
import IconPlus from '@/components/icons/IconPlus.vue'
import IconSend from '@/components/icons/IconSend.vue'
import IconStop from '@/components/icons/IconStop.vue'
import IconTrash from '@/components/icons/IconTrash.vue'
import IconZap from '@/components/icons/IconZap.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import StateBanner from '@/components/common/StateBanner.vue'
import ConfirmModal from '@/components/common/ConfirmModal.vue'

const { t } = useI18n()
const chats = useChatsStore()
const ui = useUiStore()

const COMMANDS = ['compact', 'clear', 'context', 'export'] as const
type SlashCommand = (typeof COMMANDS)[number]

const draft = ref('')
const sessionMenuOpen = ref(false)
const editingChatId = ref<string | null>(null)
const editingTitle = ref('')
const slashOpen = ref(false)
const slashIndex = ref(0)
const contextEditorOpen = ref(false)
const contextDraft = ref('')
const contextSaved = ref(false)
const compactNotice = ref(false)
const deleteTargetId = ref<string | null>(null)
const messagesEl = ref<HTMLElement | null>(null)

const filteredCommands = computed<SlashCommand[]>(() => {
  if (!slashOpen.value) return []
  const prefix = draft.value.slice(1).toLowerCase()
  return COMMANDS.filter((command) => command.startsWith(prefix))
})

const deleteTargetTitle = computed(
  () => chats.chats.find((chat) => chat.id === deleteTargetId.value)?.title ?? '',
)

onMounted(() => {
  void chats.ensureLoaded()
})

watch(
  () => [chats.activeMessages.length, chats.streamingMessage?.content, contextEditorOpen.value],
  () => {
    void nextTick(() => {
      const el = messagesEl.value
      if (el) el.scrollTop = el.scrollHeight
    })
  },
)

function updateSlashMenu(): void {
  const value = draft.value
  if (value.startsWith('/') && !/\s/.test(value)) {
    slashOpen.value = true
    slashIndex.value = 0
  } else {
    slashOpen.value = false
  }
}

function onComposerKeydown(event: KeyboardEvent): void {
  if (slashOpen.value && filteredCommands.value.length > 0) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      slashIndex.value = (slashIndex.value + 1) % filteredCommands.value.length
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      slashIndex.value =
        (slashIndex.value - 1 + filteredCommands.value.length) % filteredCommands.value.length
      return
    }
  }
  // Enter sends; Shift+Enter inserts a newline (docs/00 §7 / docs/03 §9).
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
    event.preventDefault()
    if (slashOpen.value && filteredCommands.value.length > 0) {
      runCommand(filteredCommands.value[slashIndex.value])
    } else {
      send()
    }
  }
}

function send(): void {
  const content = draft.value.trim()
  if (content.length === 0 || chats.isGenerating) return
  draft.value = ''
  slashOpen.value = false
  void chats.send(content)
}

function runCommand(command: SlashCommand): void {
  draft.value = ''
  slashOpen.value = false
  if (command === 'clear') {
    if (chats.activeChatId) void chats.clearMessages(chats.activeChatId)
    return
  }
  if (command === 'context') {
    void openContextEditor()
    return
  }
  if (command === 'compact') {
    compactNotice.value = true
    window.setTimeout(() => {
      compactNotice.value = false
    }, 4000)
    return
  }
  if (command === 'export') {
    exportMarkdown()
  }
}

async function openContextEditor(): Promise<void> {
  contextDraft.value = await chats.loadConversationContext()
  contextSaved.value = false
  contextEditorOpen.value = true
}

async function saveContext(): Promise<void> {
  await chats.saveConversationContext(contextDraft.value)
  contextEditorOpen.value = false
  contextSaved.value = true
  window.setTimeout(() => {
    contextSaved.value = false
  }, 2500)
}

function exportMarkdown(): void {
  const chat = chats.activeChat
  if (!chat) return
  const lines: string[] = [`# ${chat.title}`, '']
  for (const message of chats.activeMessages) {
    lines.push(`## ${message.role === 'user' ? 'User' : 'Assistant'}`, '', message.content, '')
    if (message.reasoning_content) {
      lines.push('> Reasoning: ' + message.reasoning_content, '')
    }
  }
  const blob = new Blob([lines.join('\n')], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `${chat.title}.md`
  anchor.click()
  URL.revokeObjectURL(url)
}

function startRename(id: string, currentTitle: string): void {
  editingChatId.value = id
  editingTitle.value = currentTitle
}

async function commitRename(): Promise<void> {
  const id = editingChatId.value
  const title = editingTitle.value.trim()
  if (id && title.length > 0) {
    await chats.renameChat(id, title)
  }
  editingChatId.value = null
}

function selectChat(id: string): void {
  sessionMenuOpen.value = false
  if (id !== chats.activeChatId) void chats.selectChat(id)
}

async function createChat(): Promise<void> {
  sessionMenuOpen.value = false
  await chats.newChat()
}

async function confirmDeleteChat(): Promise<void> {
  if (deleteTargetId.value) await chats.deleteChat(deleteTargetId.value)
  deleteTargetId.value = null
  if (chats.chats.length === 0) await chats.newChat()
}
</script>

<template>
  <aside class="ai-sidebar" :aria-label="t('ai.title')">
    <header class="ai-header">
      <div class="session-switcher">
        <button
          type="button"
          class="session-btn"
          aria-haspopup="listbox"
          :aria-expanded="sessionMenuOpen"
          data-testid="session-switcher"
          @click="sessionMenuOpen = !sessionMenuOpen"
        >
          <span class="session-title">{{ chats.activeChat?.title ?? t('ai.sessions') }}</span>
          <IconChevronDown :size="14" />
        </button>
        <div v-if="sessionMenuOpen" class="session-menu">
          <button
            v-for="chat in chats.chats"
            :key="chat.id"
            type="button"
            class="session-item"
            :class="{ active: chat.id === chats.activeChatId }"
            @click="selectChat(chat.id)"
          >
            <template v-if="editingChatId === chat.id">
              <input
                v-model="editingTitle"
                class="input session-rename-input"
                :aria-label="t('ai.renameChat')"
                @click.stop
                @keydown.enter.prevent="commitRename"
                @keydown.esc="editingChatId = null"
                @blur="commitRename"
              />
            </template>
            <template v-else>
              <span class="session-item-title">{{ chat.title }}</span>
              <span class="session-item-actions" @click.stop>
                <IconButton
                  :label="t('ai.renameChat')"
                  size="sm"
                  @click="startRename(chat.id, chat.title)"
                >
                  <IconEdit :size="12" />
                </IconButton>
                <IconButton
                  :label="t('ai.deleteChat')"
                  size="sm"
                  tone="danger"
                  @click="deleteTargetId = chat.id"
                >
                  <IconTrash :size="12" />
                </IconButton>
              </span>
            </template>
          </button>
          <button type="button" class="session-new" @click="createChat()">
            <IconPlus :size="14" />
            {{ t('ai.newChat') }}
          </button>
        </div>
      </div>
      <IconButton :label="t('common.close')" @click="ui.sidebarOpen = false">
        <IconClose :size="16" />
      </IconButton>
    </header>

    <div ref="messagesEl" class="ai-messages">
      <EmptyState
        v-if="chats.activeMessages.length === 0 && !chats.streamingMessage"
        :title="t('ai.emptyTitle')"
        :description="t('ai.emptyDesc')"
      />
      <template v-else>
        <div
          v-for="message in chats.activeMessages"
          :key="message.id"
          class="message"
          :class="`message-${message.role}`"
          data-testid="chat-message"
        >
          <details v-if="message.reasoning_content" class="reasoning">
            <summary>{{ t('ai.reasoning') }}</summary>
            <p class="reasoning-body">{{ message.reasoning_content }}</p>
          </details>
          <div class="message-content">{{ message.content }}</div>
        </div>
      </template>

      <div
        v-if="chats.streamingMessage"
        class="message message-assistant streaming"
        data-testid="chat-streaming"
      >
        <details v-if="chats.streamingMessage.reasoning_content" class="reasoning">
          <summary>{{ t('ai.reasoning') }}</summary>
          <p class="reasoning-body">{{ chats.streamingMessage.reasoning_content }}</p>
        </details>
        <div class="message-content">
          {{ chats.streamingMessage.content }}<span class="stream-cursor" aria-hidden="true" />
        </div>
      </div>

      <p v-if="chats.isGenerating" class="system-note">{{ t('ai.streaming') }}</p>
      <p v-if="chats.generationStatus === 'cancelled'" class="system-note">
        {{ t('ai.cancelled') }}
      </p>
      <StateBanner
        v-if="chats.generationStatus === 'error' && chats.generationError"
        variant="error"
        :title="t('ai.generationError')"
        :message="chats.generationError"
        class="message-error"
      />
      <StateBanner
        v-if="compactNotice"
        variant="info"
        :message="t('ai.compactNotice')"
        class="message-error"
      />
      <StateBanner
        v-if="contextSaved"
        variant="success"
        :message="t('ai.contextSaved')"
        class="message-error"
      />

      <div v-if="contextEditorOpen" class="context-editor">
        <p class="context-title">{{ t('ai.contextEditorTitle') }}</p>
        <textarea
          v-model="contextDraft"
          class="textarea"
          rows="5"
          :placeholder="t('ai.contextEditorPlaceholder')"
          :aria-label="t('ai.contextEditorTitle')"
        />
        <div class="context-actions">
          <button type="button" class="btn btn-secondary" @click="contextEditorOpen = false">
            {{ t('common.cancel') }}
          </button>
          <button type="button" class="btn btn-primary" @click="saveContext">
            {{ t('common.save') }}
          </button>
        </div>
      </div>
    </div>

    <footer class="ai-composer">
      <div v-if="slashOpen && filteredCommands.length > 0" class="slash-menu" data-testid="slash-menu">
        <p class="slash-hint">{{ t('ai.commandHint') }}</p>
        <button
          v-for="(command, index) in filteredCommands"
          :key="command"
          type="button"
          class="slash-item"
          :class="{ highlighted: index === slashIndex }"
          :data-testid="`slash-${command}`"
          @click="runCommand(command)"
        >
          <code class="slash-command">/{{ command }}</code>
          <span class="slash-desc">{{ t(`ai.commands.${command}`) }}</span>
        </button>
      </div>

      <textarea
        v-model="draft"
        class="composer-input"
        rows="2"
        data-testid="ai-composer"
        :placeholder="t('ai.composerPlaceholder')"
        :aria-label="t('ai.composerPlaceholder')"
        @input="updateSlashMenu"
        @keydown="onComposerKeydown"
      />

      <div class="composer-row">
        <button
          type="button"
          class="thinking-toggle"
          :class="{ on: chats.thinkingEnabled }"
          :aria-pressed="chats.thinkingEnabled"
          :aria-label="t('ai.thinking')"
          @click="chats.thinkingEnabled = !chats.thinkingEnabled"
        >
          <IconZap :size="13" />
          {{ t('ai.thinking') }}
        </button>
        <span class="composer-spacer" />
        <IconButton
          v-if="chats.isGenerating"
          :label="t('ai.stop')"
          tone="danger"
          data-testid="ai-stop"
          @click="chats.stop()"
        >
          <IconStop :size="16" />
        </IconButton>
        <button
          v-else
          type="button"
          class="btn btn-primary composer-send"
          :disabled="draft.trim().length === 0"
          data-testid="ai-send"
          :aria-label="t('ai.send')"
          @click="send"
        >
          <IconSend :size="15" />
        </button>
      </div>
    </footer>

    <ConfirmModal
      :open="deleteTargetId !== null"
      :title="t('ai.deleteChat')"
      :message="`${t('ai.deleteChatMessage')}（${deleteTargetTitle}）`"
      :confirm-label="t('common.delete')"
      @confirm="confirmDeleteChat"
      @cancel="deleteTargetId = null"
    />
  </aside>
</template>

<style scoped>
.ai-sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.ai-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--hairline);
  flex: none;
}

.session-switcher {
  position: relative;
  min-width: 0;
}

.session-btn {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  max-width: 260px;
  padding: 6px 10px;
  border-radius: var(--radius-md);
  color: var(--text-primary);
  font-weight: 600;
  font-size: 13px;
}

.session-btn:hover {
  background: var(--bg-hover);
}

.session-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  width: min(280px, 80vw);
  max-height: 320px;
  overflow-y: auto;
  background: var(--bg-surface);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-2);
  padding: var(--space-1);
  z-index: 20;
}

.session-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  width: 100%;
  padding: 7px 10px;
  border-radius: var(--radius-md);
  font-size: 13px;
  text-align: left;
}

.session-item:hover,
.session-item.active {
  background: var(--bg-hover);
}

.session-item-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-item-actions {
  display: none;
  align-items: center;
  gap: 2px;
  flex: none;
}

.session-item:hover .session-item-actions,
.session-item.active .session-item-actions {
  display: inline-flex;
}

.session-rename-input {
  padding: 3px 8px;
  font-size: 13px;
}

.session-new {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  font-size: 13px;
  color: var(--accent);
}

.session-new:hover {
  background: var(--accent-soft);
}

.ai-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.message {
  font-size: 13px;
  line-height: 1.65;
}

.message-user {
  align-self: flex-end;
  max-width: 88%;
  background: var(--accent-soft);
  color: var(--text-primary);
  border-radius: var(--radius-lg);
  border-bottom-right-radius: 6px;
  padding: var(--space-2) var(--space-3);
}

.message-user .message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.message-assistant {
  align-self: stretch;
  color: var(--text-primary);
}

.message-assistant .message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.reasoning {
  margin-bottom: var(--space-2);
  border-left: 2px solid var(--hairline-strong);
  padding-left: var(--space-3);
  color: var(--text-secondary);
}

.reasoning summary {
  font-size: 12px;
  cursor: pointer;
  color: var(--text-tertiary);
  user-select: none;
}

.reasoning-body {
  font-size: 12px;
  white-space: pre-wrap;
  margin-top: var(--space-1);
}

.stream-cursor {
  display: inline-block;
  width: 7px;
  height: 14px;
  margin-left: 2px;
  vertical-align: -2px;
  background: var(--accent);
  border-radius: 2px;
  animation: blink 0.9s steps(1) infinite;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

.system-note {
  align-self: center;
  font-size: 12px;
  color: var(--text-tertiary);
}

.message-error {
  align-self: stretch;
}

.context-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-lg);
  background: var(--bg-surface);
}

.context-title {
  font-size: 13px;
  font-weight: 600;
}

.context-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}

.ai-composer {
  flex: none;
  position: relative;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3);
  border-top: 1px solid var(--hairline);
  background: var(--bg-surface-2);
}

.slash-menu {
  position: absolute;
  bottom: calc(100% + 6px);
  left: var(--space-3);
  right: var(--space-3);
  background: var(--bg-surface);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-2);
  padding: var(--space-1);
  z-index: 20;
  max-height: 220px;
  overflow-y: auto;
}

.slash-hint {
  font-size: 11px;
  color: var(--text-tertiary);
  padding: 6px 10px 2px;
}

.slash-item {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  width: 100%;
  padding: 7px 10px;
  border-radius: var(--radius-md);
  text-align: left;
  font-size: 13px;
}

.slash-item.highlighted,
.slash-item:hover {
  background: var(--bg-hover);
}

.slash-command {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--accent);
  flex: none;
}

.slash-desc {
  color: var(--text-secondary);
}

.composer-input {
  width: 100%;
  border: 1px solid var(--hairline-strong);
  border-radius: var(--radius-lg);
  background: var(--bg-surface);
  padding: var(--space-2) var(--space-3);
  font-size: 13px;
  line-height: 1.6;
}

.composer-input:focus-visible {
  border-color: var(--accent);
  outline: none;
}

.composer-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.composer-spacer {
  flex: 1;
}

.thinking-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 12px;
  color: var(--text-secondary);
  border: 1px solid var(--hairline-strong);
  transition:
    color var(--transition-fast),
    background var(--transition-fast),
    border-color var(--transition-fast);
}

.thinking-toggle.on {
  color: var(--accent);
  border-color: var(--accent);
  background: var(--accent-soft);
}

.composer-send {
  padding: 8px;
  border-radius: var(--radius-md);
}
</style>
