/**
 * Routes: /onboarding, /translate (default), /vocabulary, /settings.
 * History drawer + AI sidebar are overlays, not routes (docs/03 §2).
 */
import { createRouter, createWebHashHistory } from 'vue-router'
import { useSettingsStore } from '@/stores/settings'

const TranslatePage = () => import('@/components/translate/TranslatePage.vue')
const VocabularyPage = () => import('@/components/vocabulary/VocabularyPage.vue')
const SettingsPage = () => import('@/components/settings/SettingsPage.vue')
const OnboardingWizard = () => import('@/components/onboarding/OnboardingWizard.vue')

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/onboarding',
      name: 'onboarding',
      component: OnboardingWizard,
      meta: { bare: true },
    },
    { path: '/translate', name: 'translate', component: TranslatePage },
    { path: '/vocabulary', name: 'vocabulary', component: VocabularyPage },
    { path: '/settings', name: 'settings', component: SettingsPage },
    { path: '/', redirect: '/translate' },
    { path: '/:pathMatch(.*)*', redirect: '/translate' },
  ],
})

router.beforeEach(async (to) => {
  const settings = useSettingsStore()
  if (settings.status !== 'success') {
    await settings.load()
  }
  // First-run wizard shows only when no API key is configured (real mode).
  if (settings.needsOnboarding) {
    return to.name === 'onboarding' ? true : { name: 'onboarding' }
  }
  if (to.name === 'onboarding') {
    return { name: 'translate' }
  }
  return true
})
