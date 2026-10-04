<script setup lang="ts">
/**
 * Settings page (docs/03 §10): General / Provider / AI Context / Translation
 * / Network / Data / About. Mock mode is shown subtly next to the title.
 * Supports ?section=provider deep links (e.g. the translate page CTA).
 */
import { nextTick, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useSettingsStore } from '@/stores/settings'
import GeneralSection from './GeneralSection.vue'
import ProviderSection from './ProviderSection.vue'
import AiContextSection from './AiContextSection.vue'
import TranslationSection from './TranslationSection.vue'
import NetworkSection from './NetworkSection.vue'
import DataSection from './DataSection.vue'
import AboutSection from './AboutSection.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import StateBanner from '@/components/common/StateBanner.vue'

const { t } = useI18n()
const route = useRoute()
const settings = useSettingsStore()

onMounted(() => {
  void settings.load()
})

watch(
  () => route.query.section,
  async (section) => {
    if (section !== 'provider') return
    await settings.load()
    await nextTick()
    document.getElementById('settings-provider')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  },
  { immediate: true },
)
</script>

<template>
  <section class="settings-page">
    <header class="settings-header">
      <h1 class="settings-title">{{ t('settings.title') }}</h1>
      <span v-if="settings.mockMode" class="badge badge-accent">{{ t('common.mockMode') }}</span>
    </header>

    <LoadingState v-if="settings.status === 'loading' || settings.status === 'idle'" />
    <StateBanner
      v-else-if="settings.status === 'error'"
      variant="error"
      :title="t('errors.unknown')"
      :message="settings.loadError ?? ''"
      show-retry
      @retry="settings.load(true)"
    />
    <template v-else>
      <GeneralSection />
      <ProviderSection />
      <AiContextSection />
      <TranslationSection />
      <NetworkSection />
      <DataSection />
      <AboutSection />
    </template>
  </section>
</template>

<style scoped>
.settings-page {
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-height: 100%;
}

.settings-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.settings-title {
  font-size: 20px;
  font-weight: 700;
}
</style>
