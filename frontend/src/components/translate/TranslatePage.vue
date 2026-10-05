<script setup lang="ts">
/**
 * Translate page: renders the active tab's per-tab translation state machine
 * — empty/input → translating → success | cache_success | retryable_error —
 * via TranslateInput / LoadingState / WordResult / TextResult / StateBanner.
 *
 * Error variants (Phase 2 contract):
 * - retryable errors show a retry button;
 * - UNSUPPORTED_LANGUAGE (and any non-retryable error) is inline without one;
 * - PROVIDER_NOT_CONFIGURED shows a "前往设置" CTA to the provider section.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useTabsStore } from '@/stores/tabs'
import { useTranslationStore } from '@/stores/translation'
import type { TranslationResponse } from '@/api/types'
import TranslateInput from './TranslateInput.vue'
import QuickTranslateInput from './QuickTranslateInput.vue'
import WordResult from './WordResult.vue'
import TextResult from './TextResult.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import StateBanner from '@/components/common/StateBanner.vue'

const { t } = useI18n()
const router = useRouter()
const tabsStore = useTabsStore()
const translationStore = useTranslationStore()

const tab = computed(() => tabsStore.activeTab)

const state = computed(() =>
  tab.value ? translationStore.stateFor(tab.value.id) : null,
)

const response = computed(() => state.value?.response ?? null)

const isWord = computed(
  () => response.value !== null && 'lemma' in response.value.result,
)

const showInput = computed(
  () => state.value !== null && response.value === null && state.value.status !== 'translating',
)

const showLoading = computed(
  () => state.value !== null && state.value.status === 'translating' && response.value === null,
)

const errorState = computed(() =>
  state.value !== null && state.value.status === 'retryable_error' ? state.value.error : null,
)

const showRetry = computed(() => errorState.value?.retryable === true)
const showProviderCta = computed(() => errorState.value?.code === 'PROVIDER_NOT_CONFIGURED')

/** The source text a response was produced from (word or text result). */
function responseSource(response: TranslationResponse): string {
  return 'lemma' in response.result ? response.result.word : response.result.source_markdown
}

/**
 * Retry for a pure failure (no result yet) re-runs the input translation; when
 * the visible result IS the failed request's own source it retranslates it
 * (bypassing the cache).
 *
 * A failed inline lookup (improvement bug #8) leaves a NEW input next to the
 * OLD result, so retrying must send the typed text again instead of re-running
 * the previous translation id.
 */
function onRetry(): void {
  if (!tab.value || !state.value) return
  const current = response.value
  const retrySameResult =
    current !== null &&
    current.translation_id.length > 0 &&
    state.value.input.trim() === responseSource(current).trim()
  if (retrySameResult) {
    void translationStore.retranslate(tab.value.id)
  } else {
    void translationStore.translate(tab.value.id)
  }
}

function goToSettings(): void {
  void router.push({ path: '/settings', query: { section: 'provider' } })
}
</script>

<template>
  <section class="translate-page">
    <template v-if="tab && state">
      <StateBanner
        v-if="response && state.status === 'cache_success'"
        variant="cache"
        :title="t('translate.cacheBannerTitle')"
        :message="t('translate.cacheBannerDesc')"
        data-testid="cache-banner"
      />
      <StateBanner
        v-else-if="errorState"
        variant="error"
        :title="t('translate.errorTitle')"
        :message="errorState.message"
        :show-retry="showRetry"
        data-testid="translate-error-banner"
        @retry="onRetry"
      >
        <button
          v-if="showProviderCta"
          type="button"
          class="btn btn-secondary banner-cta"
          data-testid="go-to-settings"
          @click="goToSettings"
        >
          {{ t('translate.goToSettings') }}
        </button>
      </StateBanner>

      <LoadingState v-if="showLoading" :label="t('translate.translating')" />

      <TranslateInput v-else-if="showInput" :tab-id="tab.id" />

      <template v-else-if="response">
        <!-- improvement bug #8: look the next word up in place, right above
             the result, instead of opening another tab. -->
        <QuickTranslateInput :tab-id="tab.id" />
        <WordResult v-if="isWord" :response="response" />
        <TextResult v-else :response="response" />
      </template>
    </template>
  </section>
</template>

<style scoped>
.translate-page {
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-height: 100%;
}

.banner-cta {
  margin-top: var(--space-2);
  font-size: 12px;
}
</style>
