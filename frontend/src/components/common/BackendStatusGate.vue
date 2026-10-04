<script setup lang="ts">
/**
 * Full-screen backend availability gate (real mode only, docs/01 §3 /
 * Phase 2 contract): starting → spinner, restarting → notice, failed →
 * error with retry. Mock mode never mounts this component.
 */
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useBackendStore } from '@/stores/backend'
import { useSettingsStore } from '@/stores/settings'
import StateBanner from './StateBanner.vue'
import LoadingState from './LoadingState.vue'

const { t } = useI18n()
const router = useRouter()
const backend = useBackendStore()
const settings = useSettingsStore()

const visible = computed(() => backend.isRealMode && !backend.ready)

const showSpinner = computed(() => backend.status === 'starting')
const showRestarting = computed(() => backend.status === 'restarting')
const showFailed = computed(() => backend.status === 'failed')

// While the gate is up, navigation is blocked (router guard). Once the
// backend becomes ready, refresh app data and replay the pending route so
// the guard re-runs with a working backend (onboarding vs main UI).
watch(
  () => backend.ready,
  (ready) => {
    if (!ready) return
    void settings.load(true).finally(() => {
      const target = window.location.hash.slice(1) || '/'
      void router.replace(target).catch(() => undefined)
    })
  },
)

async function onRetry(): Promise<void> {
  await backend.retry()
}
</script>

<template>
  <div v-if="visible" class="backend-gate" data-testid="backend-gate">
    <div class="gate-card card">
      <LoadingState v-if="showSpinner" :label="t('backend.startingTitle')" />
      <StateBanner
        v-else-if="showRestarting"
        variant="info"
        :title="t('backend.restartingTitle')"
        :message="t('backend.startingDesc')"
      />
      <template v-else-if="showFailed">
        <StateBanner
          variant="error"
          :title="t('backend.failedTitle')"
          :message="t('backend.failedDesc')"
          show-retry
          :retry-label="t('common.retry')"
          data-testid="backend-gate-error"
          @retry="onRetry"
        />
        <p class="gate-hint">{{ t('backend.failedHint') }}</p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.backend-gate {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-5);
  background: var(--bg-app);
}

.gate-card {
  width: min(360px, 100%);
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.gate-hint {
  font-size: 12px;
  color: var(--text-tertiary);
  line-height: 1.6;
}
</style>
