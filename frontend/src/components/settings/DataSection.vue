<script setup lang="ts">
/**
 * Data settings: backup export/import placeholders (Tauri file picker comes
 * with the host), clear business data, and reset app with double confirm
 * (docs/00 §9).
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useApi } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import { useChatsStore } from '@/stores/chats'
import { useVocabularyStore } from '@/stores/vocabulary'
import ConfirmModal from '@/components/common/ConfirmModal.vue'

const { t } = useI18n()
const router = useRouter()
const settings = useSettingsStore()
const chats = useChatsStore()
const vocabulary = useVocabularyStore()

const notice = ref<string | null>(null)
const clearConfirmOpen = ref(false)
const resetConfirm1Open = ref(false)
const resetConfirm2Open = ref(false)
const working = ref(false)

function showNotice(message: string): void {
  notice.value = message
  window.setTimeout(() => {
    notice.value = null
  }, 4000)
}

function onBackupClick(): void {
  showNotice(t('common.desktopOnly'))
}

async function clearBusiness(): Promise<void> {
  working.value = true
  try {
    await useApi().clearBusinessData()
    chats.resetLocalState()
    await vocabulary.load()
    showNotice(t('common.saved'))
  } finally {
    working.value = false
    clearConfirmOpen.value = false
  }
}

async function resetApp(): Promise<void> {
  working.value = true
  try {
    await useApi().resetApp()
    chats.resetLocalState()
    settings.invalidate()
    await settings.load(true)
    await router.push('/translate')
  } finally {
    working.value = false
    resetConfirm2Open.value = false
    resetConfirm1Open.value = false
  }
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.data') }}</h2>

    <p v-if="notice" class="notice">{{ notice }}</p>

    <div class="actions">
      <button type="button" class="btn btn-secondary" @click="onBackupClick">
        {{ t('settings.data.exportBackup') }}
      </button>
      <button type="button" class="btn btn-secondary" @click="onBackupClick">
        {{ t('settings.data.importBackup') }}
      </button>
    </div>
    <p class="hint">{{ t('settings.data.backupDesc') }}</p>

    <div class="danger-zone">
      <button
        type="button"
        class="btn btn-danger-ghost"
        :disabled="working"
        @click="clearConfirmOpen = true"
      >
        {{ t('settings.data.clearBusiness') }}
      </button>
      <button
        type="button"
        class="btn btn-danger"
        :disabled="working"
        @click="resetConfirm1Open = true"
      >
        {{ t('settings.data.resetApp') }}
      </button>
    </div>

    <ConfirmModal
      :open="clearConfirmOpen"
      :title="t('settings.data.clearBusiness')"
      :message="t('settings.data.clearBusinessMessage')"
      :confirm-label="t('settings.data.clearBusiness')"
      @confirm="clearBusiness"
      @cancel="clearConfirmOpen = false"
    />
    <ConfirmModal
      :open="resetConfirm1Open"
      :title="t('settings.data.resetApp')"
      :message="t('settings.data.resetMessage1')"
      :confirm-label="t('common.confirm')"
      @confirm="resetConfirm1Open = false; resetConfirm2Open = true"
      @cancel="resetConfirm1Open = false"
    />
    <ConfirmModal
      :open="resetConfirm2Open"
      :title="t('settings.data.resetApp')"
      :message="t('settings.data.resetMessage2')"
      :confirm-label="t('settings.data.resetApp')"
      @confirm="resetApp"
      @cancel="resetConfirm2Open = false"
    />
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.notice {
  font-size: 12px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  background: var(--accent-soft);
  color: var(--accent);
}

.actions {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.hint {
  font-size: 11px;
  color: var(--text-tertiary);
  line-height: 1.6;
}

.danger-zone {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  padding-top: var(--space-3);
  border-top: 1px solid var(--hairline);
}
</style>
