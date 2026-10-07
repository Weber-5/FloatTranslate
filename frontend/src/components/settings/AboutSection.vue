<script setup lang="ts">
/**
 * About (docs/00 §9, docs/10 §5): version, check update (GitHub Releases,
 * prompt-only — never silent), open logs directory, GitHub link. Shows the
 * subtle Mock-mode badge in mock mode.
 *
 * Phase 5 contract:
 * - Newer release → banner with version + "查看更新" link (rel noopener,
 *   target _blank) and the release URL as text fallback.
 * - Equal/older → "已是最新版本" toast.
 * - No published release (private repo / no release yet) → its own notice,
 *   never a false "up to date".
 * - Network failure → inline retryable error (重试 re-runs the check).
 * - 清空日志 has no dedicated endpoint in 1.0 — the row shows the logs
 *   path only (freeze decision), with open_logs_dir next to it.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { getBackendConfig } from '@/api'
import { APP_VERSION } from '@/constants'
import { checkForUpdate, UpdateCheckError, type LatestRelease } from '@/services/update'
import { openLogsDir, openExternalUrl } from '@/services/native'
import SettingRow from './SettingRow.vue'
import IconExternal from '@/components/icons/IconExternal.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const notice = ref<string | null>(null)
/** Set with a sticky notice: "checked at HH:MM:SS" (UX review 2026-10-07). */
const noticeAt = ref<string | null>(null)
let noticeTimer: number | undefined
const GITHUB_URL = 'https://github.com/Weber-5/FloatTranslate'
const RELEASE_URL = `${GITHUB_URL}/releases/latest`

/**
 * Inline notice under the row. Transient by default (4 s); the update-check
 * result is STICKY so it is still readable after the user looks away.
 */
function showNotice(message: string, sticky = false): void {
  notice.value = message
  noticeAt.value = sticky ? new Date().toLocaleTimeString() : null
  if (noticeTimer !== undefined) window.clearTimeout(noticeTimer)
  if (!sticky) {
    noticeTimer = window.setTimeout(() => {
      if (notice.value === message) {
        notice.value = null
        noticeAt.value = null
      }
    }, 4000)
  }
}

// ---- Update check -----------------------------------------------------------------

const checking = ref(false)
const updateError = ref<string | null>(null)
/** The newer release when `update-available`; null otherwise. */
const availableUpdate = ref<LatestRelease | null>(null)

async function runUpdateCheck(): Promise<void> {
  if (checking.value) return
  checking.value = true
  updateError.value = null
  // A previous result must not look like the current one.
  notice.value = null
  noticeAt.value = null
  try {
    const result = await checkForUpdate(APP_VERSION)
    if (result.status === 'update-available' && result.latest) {
      availableUpdate.value = result.latest
    } else {
      availableUpdate.value = null
      // improvement bug #7: a repo without a published release is not the same
      // as "you are up to date" — say which one it is, and keep it on screen.
      showNotice(
        result.status === 'no-release'
          ? t('settings.about.noRelease')
          : t('settings.about.upToDate'),
        true,
      )
    }
  } catch (error) {
    availableUpdate.value = null
    updateError.value =
      error instanceof UpdateCheckError && error.message === 'timeout'
        ? t('settings.about.updateTimeout')
        : t('settings.about.updateCheckFailed')
  } finally {
    checking.value = false
  }
}

// ---- Logs --------------------------------------------------------------------------

const logsPath = computed(() => {
  const config = getBackendConfig()
  if (!config) return null
  return `${config.data_root.replace(/[\\/]+$/, '')}\\logs`
})

async function onOpenLogs(): Promise<void> {
  const result = await openLogsDir()
  showNotice(result.ok ? t('settings.about.logsOpened') : t('common.unsupported'))
}

/**
 * improvement bug #7: the packaged WebView denies `target="_blank"` popups,
 * so external links are opened by the host instead. The anchor stays in the
 * DOM (href copyable, testable) and only its default navigation is cancelled.
 */
async function onOpenExternal(url: string, event: MouseEvent): Promise<void> {
  event.preventDefault()
  const result = await openExternalUrl(url)
  if (!result.ok) showNotice(t('common.unsupported'))
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.about') }}</h2>

    <SettingRow :label="t('settings.about.version')">
      <span class="version" data-testid="app-version">v{{ APP_VERSION }}</span>
    </SettingRow>

    <SettingRow :label="t('common.appName')">
      <span v-if="settings.mockMode" class="badge badge-accent">{{ t('common.mockMode') }}</span>
    </SettingRow>

    <SettingRow :label="t('settings.about.checkUpdate')">
      <button
        type="button"
        class="btn btn-secondary"
        data-testid="check-update"
        :disabled="checking"
        @click="runUpdateCheck"
      >
        {{ checking ? t('settings.about.checking') : t('settings.about.checkUpdate') }}
      </button>
    </SettingRow>

    <p
      v-if="availableUpdate"
      class="update-banner"
      role="status"
      data-testid="update-banner"
    >
      <span>{{ t('settings.about.updateAvailable', { version: availableUpdate.tag_name }) }}</span>
      <a
        :href="availableUpdate.html_url ?? RELEASE_URL"
        target="_blank"
        rel="noopener noreferrer"
        class="update-link"
        data-testid="update-link"
        @click="onOpenExternal(availableUpdate.html_url ?? RELEASE_URL, $event)"
      >
        {{ t('settings.about.viewUpdate') }}
        <IconExternal :size="12" />
      </a>
      <span class="release-url">{{ availableUpdate.html_url ?? RELEASE_URL }}</span>
    </p>

    <p v-if="updateError" class="update-error" role="alert" data-testid="update-error">
      <span>{{ updateError }}</span>
      <button type="button" class="btn btn-secondary btn-sm" data-testid="update-retry" @click="runUpdateCheck">
        {{ t('common.retry') }}
      </button>
    </p>

    <SettingRow :label="t('settings.about.logs')">
      <button type="button" class="btn btn-secondary" data-testid="open-logs" @click="onOpenLogs">
        {{ t('settings.about.logs') }}
      </button>
    </SettingRow>
    <p class="logs-path" data-testid="logs-path">
      {{
        logsPath
          ? t('settings.about.logsPath', { path: logsPath })
          : t('settings.about.logsHint')
      }}
    </p>

    <SettingRow :label="t('settings.about.github')">
      <a
        :href="GITHUB_URL"
        target="_blank"
        rel="noopener noreferrer"
        class="github-link"
        data-testid="github-link"
        @click="onOpenExternal(GITHUB_URL, $event)"
      >
        {{ t('settings.about.github') }}
        <IconExternal :size="13" />
      </a>
    </SettingRow>

    <p v-if="notice" class="notice" role="status" data-testid="about-notice">
      {{ notice }}
      <span v-if="noticeAt" class="notice-time">{{ t('settings.about.checkedAt', { time: noticeAt }) }}</span>
    </p>
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.version {
  font-family: var(--font-mono);
  font-size: 12px;
}

.notice {
  font-size: 12px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  background: var(--success-soft);
  color: var(--success);
}

.update-banner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  font-size: 12px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  background: var(--accent-soft);
  color: var(--accent);
}

.update-link {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--accent);
  font-weight: 600;
}

.release-url {
  flex-basis: 100%;
  word-break: break-all;
  color: var(--text-tertiary);
}

.update-error {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: 12px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  background: var(--danger-soft);
  color: var(--danger);
}

.btn-sm {
  font-size: 11px;
  padding: 2px 8px;
}

.logs-path {
  font-size: 11px;
  color: var(--text-tertiary);
  line-height: 1.6;
  word-break: break-all;
}

.github-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
}
</style>
