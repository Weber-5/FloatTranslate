<script setup lang="ts">
/**
 * Ask AI: opens the AI Sidebar and sends the current content into the active
 * session as a quoted reference (docs/00 §7 / docs/06 §12).
 */
import { useI18n } from 'vue-i18n'
import { useChatsStore } from '@/stores/chats'
import { useUiStore } from '@/stores/ui'

const props = defineProps<{ text: string }>()

const { t } = useI18n()
const chats = useChatsStore()
const ui = useUiStore()

function onAsk(): void {
  const content = props.text.trim()
  if (content.length === 0) return
  ui.sidebarOpen = true
  void chats.askAi(content)
}
</script>

<template>
  <button type="button" class="btn btn-ghost ask-ai" @click="onAsk">
    {{ t('translate.askAi') }}
  </button>
</template>

<style scoped>
.ask-ai {
  font-weight: 500;
}
</style>
