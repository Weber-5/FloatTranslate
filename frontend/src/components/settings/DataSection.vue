<script setup lang="ts">
/**
 * Data settings (docs/00 §9): JSON backup export/import via the Tauri file
 * picker + backend endpoints, clear business data, and reset app with the
 * frozen double confirm (docs/05 §7).
 *
 * Flows (Phase 5 contract):
 * - Export: pick_save_path(default floattranslate-backup-YYYYMMDD-HHmmss.json)
 *   → POST /backup/export { path } → success notice with the path.
 * - Import: pick_open_path(.json) → POST /backup/import { path } → success
 *   notice with per-table counts; BACKUP_VERSION_UNSUPPORTED gets a dedicated
 *   "please upgrade" message; other errors render inline.
 * - Reset: two sequential ConfirmModals → resetApp → reset_window_state
 *   (best-effort) → refetch all app state → router to /onboarding.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useApi, toApiError } from '@/api'
import type { BackupImportCounts } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import { useChatsStore } from '@/stores/chats'
import { useVocabularyStore } from '@/stores/vocabulary'
import { pickSavePath, pickOpenPath, resetWindowState } from '@/services/native'
import ConfirmModal from '@/components/common/ConfirmModal.vue'

const { t } = useI18n()
const router = useRouter()
const settings = useSettingsStore()
const chats = useChatsStore()
const vocabulary = useVocabularyStore()

const notice = ref<string | null>(null)
const error = ref<string | null>(null)
const clearConfirmOpen = ref(false)
const resetConfirm1Open = ref(false)
const resetConfirm2Open = ref(false)
const working = ref(false)

function showNotice(message: string): void {
  notice.value = message
  error.value = null
  window.setTimeout(() => {
    if (notice.value === message) notice.value = null
  }, 6000)
}

function showError(message: string): void {
  error.value = message
  notice.value = null
}

/** Local-time stamp for the default backup file name: YYYYMMDD-HHmmss. */
function backupStamp(date = new Date()): string {
  const pad = (value: number): string => String(value).padStart(2, '0')
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  )
}

/** 导出备份：picker → POST /backup/export → success notice with the path. */
async function exportBackup(): Promise<void> {
  if (working.value) return
  const pick = await pickSavePath({
    default_file_name: `floattranslate-backup-${backupStamp()}.json`,
    filter_name: 'JSON',
    filter_ext: 'json',
  })
  if (pick.unsupported) {
    showError(t('common.unsupported'))
    return
  }
  if (pick.cancelled || !pick.ok || !pick.path) return // user cancelled: no call
  working.value = true
  try {
    const result = await useApi().exportBackup(pick.path)
    showNotice(t('settings.data.exportSuccess', { path: result.path ?? pick.path }))
  } catch (err) {
    showError(t('settings.data.actionFailed', { message: toApiError(err).message }))
  } finally {
    working.value = false
  }
}

function importSummary(counts: BackupImportCounts): string {
  return t('settings.data.importSummary', {
    history: counts.translation_history ?? 0,
    vocabulary: counts.vocabulary ?? 0,
    terminology: counts.terminology ?? 0,
    chats: counts.chats ?? 0,
    messages: counts.messages ?? 0,
  })
}

/** 导入备份：picker → POST /backup/import → counts summary / upgrade hint. */
async function importBackup(): Promise<void> {
  if (working.value) return
  const pick = await pickOpenPath({
    filter_name: 'JSON',
    filter_ext: 'json',
  })
  if (pick.unsupported) {
    showError(t('common.unsupported'))
    return
  }
  if (pick.cancelled || !pick.ok || !pick.path) return // user cancelled: no call
  working.value = true
  try {
    const result = await useApi().importBackup(pick.path)
    showNotice(t('settings.data.importSuccess', { summary: importSummary(result.imported ?? {}) }))
    // Imported rows may affect every store; refresh the visible ones.
    chats.resetLocalState()
    await vocabulary.load()
  } catch (err) {
    const apiError = toApiError(err)
    if (apiError.code === 'BACKUP_VERSION_UNSUPPORTED') {
      showError(t('settings.data.importVersionUnsupported'))
    } else {
      showError(t('settings.data.actionFailed', { message: apiError.message }))
    }
  } finally {
    working.value = false
  }
}

async function clearBusiness(): Promise<void> {
  working.value = true
  try {
    await useApi().clearBusinessData()
    chats.resetLocalState()
    await vocabulary.load()
    showNotice(t('common.saved'))
  } catch (err) {
    showError(t('settings.data.actionFailed', { message: toApiError(err).message }))
  } finally {
    working.value = false
    clearConfirmOpen.value = false
  }
}

/**
 * 重置应用 (docs/05 §7): backend wipes settings + credential; the host clears
 * the saved window geometry (best-effort); the frontend refetches everything
 * and lands on the first-run wizard. If the key is somehow still configured
 * the router guard redirects to /translate instead.
 */
async function resetApp(): Promise<void> {
  working.value = true
  try {
    await useApi().resetApp()
    // reset_window_state is fire-and-forget: a missing/failed host command
    // must never block the reset (contract: ignore failure).
    await resetWindowState().catch(() => null)
    chats.resetLocalState()
    await vocabulary.load()
    settings.invalidate()
    await settings.load(true)
    await router.push('/onboarding')
  } catch (err) {
    showError(t('settings.data.actionFailed', { message: toApiError(err).message }))
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

    <p v-if="notice" class="notice notice-success" role="status" data-testid="data-notice">
      {{ notice }}
    </p>
    <p v-if="error" class="notice notice-error" role="alert" data-testid="data-error">
      {{ error }}
    </p>

    <div class="actions">
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="working"
        data-testid="backup-export"
        @click="exportBackup"
      >
        {{ t('settings.data.exportBackup') }}
      </button>
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="working"
        data-testid="backup-import"
        @click="importBackup"
      >
        {{ t('settings.data.importBackup') }}
      </button>
    </div>
    <p class="hint">{{ t('settings.data.backupDesc') }}</p>

    <div class="danger-zone">
      <button
        type="button"
        class="btn btn-danger-ghost"
        :disabled="working"
        data-testid="clear-business"
        @click="clearConfirmOpen = true"
      >
        {{ t('settings.data.clearBusiness') }}
      </button>
      <button
        type="button"
        class="btn btn-danger"
        :disabled="working"
        data-testid="reset-app"
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
      :danger="false"
      @confirm="
        () => {
          resetConfirm1Open = false
          resetConfirm2Open = true
        }
      "
      @cancel="resetConfirm1Open = false"
    />
    <ConfirmModal
      :open="resetConfirm2Open"
      :title="t('settings.data.resetApp')"
      :message="t('settings.data.resetMessage2')"
      :confirm-label="t('settings.data.resetApp')"
      danger
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
  line-height: 1.6;
  word-break: break-all;
}

.notice-success {
  background: var(--success-soft);
  color: var(--success);
}

.notice-error {
  background: var(--danger-soft);
  color: var(--danger);
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
