<script setup lang="ts">
/**
 * General settings: theme, always-on-top, auto start (host placeholders),
 * hotkeys display-only (docs/00 §9).
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ThemeMode } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import SettingRow from './SettingRow.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const theme = computed<ThemeMode>(() => settings.app?.theme ?? 'system')
const alwaysOnTop = computed(() => settings.app?.always_on_top ?? false)
const autoStart = computed(() => settings.app?.auto_start ?? false)

const themeOptions: { value: ThemeMode; label: string }[] = [
  { value: 'system', label: 'settings.general.themeSystem' },
  { value: 'light', label: 'settings.general.themeLight' },
  { value: 'dark', label: 'settings.general.themeDark' },
]

function setTheme(mode: ThemeMode): void {
  void settings.saveApp({ theme: mode })
}

function setAlwaysOnTop(checked: boolean): void {
  void settings.saveApp({ always_on_top: checked })
}

function setAutoStart(checked: boolean): void {
  void settings.saveApp({ auto_start: checked })
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.general') }}</h2>

    <SettingRow :label="t('settings.general.theme')">
      <div class="segmented" role="radiogroup" :aria-label="t('settings.general.theme')">
        <button
          v-for="option in themeOptions"
          :key="option.value"
          type="button"
          class="segment"
          role="radio"
          :aria-checked="theme === option.value"
          :class="{ active: theme === option.value }"
          @click="setTheme(option.value)"
        >
          {{ t(option.label) }}
        </button>
      </div>
    </SettingRow>

    <SettingRow :label="t('settings.general.alwaysOnTop')" :hint="t('common.desktopOnly')">
      <label class="switch">
        <input
          type="checkbox"
          :checked="alwaysOnTop"
          :aria-label="t('settings.general.alwaysOnTop')"
          @change="setAlwaysOnTop(($event.target as HTMLInputElement).checked)"
        />
        <span class="switch-track" aria-hidden="true" />
      </label>
    </SettingRow>

    <SettingRow :label="t('settings.general.autoStart')" :hint="t('common.desktopOnly')">
      <label class="switch">
        <input
          type="checkbox"
          :checked="autoStart"
          :aria-label="t('settings.general.autoStart')"
          @change="setAutoStart(($event.target as HTMLInputElement).checked)"
        />
        <span class="switch-track" aria-hidden="true" />
      </label>
    </SettingRow>

    <SettingRow :label="t('settings.general.hotkeyToggle')">
      <kbd class="hotkey">{{ settings.app?.hotkey_toggle_window ?? 'Ctrl+Alt+Space' }}</kbd>
    </SettingRow>

    <SettingRow :label="t('settings.general.hotkeyQuick')" :hint="t('settings.general.hotkeysHint')">
      <kbd class="hotkey">{{ settings.app?.hotkey_quick_translate ?? 'Ctrl+Alt+Q' }}</kbd>
    </SettingRow>
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.segmented {
  display: flex;
  gap: 2px;
  background: var(--bg-inset);
  border-radius: var(--radius-md);
  padding: 2px;
}

.segment {
  padding: 5px 12px;
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

.switch {
  position: relative;
  display: inline-flex;
  align-items: center;
  cursor: pointer;
}

.switch input {
  position: absolute;
  opacity: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  cursor: pointer;
}

.switch-track {
  width: 38px;
  height: 22px;
  border-radius: 999px;
  background: var(--hairline-strong);
  transition: background var(--transition-fast);
  position: relative;
}

.switch-track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #fff;
  box-shadow: var(--shadow-1);
  transition: transform var(--transition-fast);
}

.switch input:checked + .switch-track {
  background: var(--success);
}

.switch input:checked + .switch-track::after {
  transform: translateX(16px);
}

.switch input:focus-visible + .switch-track {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.hotkey {
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 3px 8px;
  border-radius: 6px;
  background: var(--bg-inset);
  border: 1px solid var(--hairline);
}
</style>
