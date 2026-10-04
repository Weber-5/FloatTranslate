<script setup lang="ts">
/**
 * About: version, check update (mock in Phase 1), logs dir placeholder,
 * GitHub link. Shows the subtle Mock-mode badge in mock mode.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { APP_VERSION } from '@/constants'
import SettingRow from './SettingRow.vue'
import IconExternal from '@/components/icons/IconExternal.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const notice = ref<string | null>(null)

const GITHUB_URL = 'https://github.com/floattranslate/floattranslate'

function showNotice(message: string): void {
  notice.value = message
  window.setTimeout(() => {
    notice.value = null
  }, 4000)
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
        @click="showNotice(t('settings.about.upToDate'))"
      >
        {{ t('settings.about.checkUpdate') }}
      </button>
    </SettingRow>

    <SettingRow :label="t('settings.about.logs')" :hint="t('common.desktopOnly')">
      <button
        type="button"
        class="btn btn-secondary"
        @click="showNotice(t('common.desktopOnly'))"
      >
        {{ t('settings.about.logs') }}
      </button>
    </SettingRow>

    <SettingRow :label="t('settings.about.github')">
      <a :href="GITHUB_URL" target="_blank" rel="noopener noreferrer" class="github-link">
        {{ t('settings.about.github') }}
        <IconExternal :size="13" />
      </a>
    </SettingRow>

    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
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

.github-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
}
</style>
