<script setup lang="ts">
/**
 * Bottom navigation: exactly three entries — 翻译 / 单词本 / 设置 (docs/00 §2).
 */
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import IconGlobe from '@/components/icons/IconGlobe.vue'
import IconBook from '@/components/icons/IconBook.vue'
import IconSettings from '@/components/icons/IconSettings.vue'

const { t } = useI18n()
const route = useRoute()
</script>

<template>
  <nav class="bottom-nav" aria-label="Main">
    <router-link
      to="/translate"
      class="nav-item"
      :class="{ active: route.name === 'translate' }"
      :aria-current="route.name === 'translate' ? 'page' : undefined"
    >
      <IconGlobe :size="18" />
      <span>{{ t('nav.translate') }}</span>
    </router-link>
    <router-link
      to="/vocabulary"
      class="nav-item"
      :class="{ active: route.name === 'vocabulary' }"
      :aria-current="route.name === 'vocabulary' ? 'page' : undefined"
    >
      <IconBook :size="18" />
      <span>{{ t('nav.vocabulary') }}</span>
    </router-link>
    <router-link
      to="/settings"
      class="nav-item"
      :class="{ active: route.name === 'settings' }"
      :aria-current="route.name === 'settings' ? 'page' : undefined"
    >
      <IconSettings :size="18" />
      <span>{{ t('nav.settings') }}</span>
    </router-link>
  </nav>
</template>

<style scoped>
.bottom-nav {
  display: flex;
  flex: none;
  border-top: 1px solid var(--hairline);
  background: var(--bg-app);
  padding: 4px 8px calc(4px + env(safe-area-inset-bottom, 0px));
}

.nav-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 5px 0;
  border-radius: var(--radius-md);
  font-size: 11px;
  color: var(--text-secondary);
  transition:
    color var(--transition-fast),
    background var(--transition-fast);
}

.nav-item:hover {
  background: var(--bg-hover);
}

.nav-item.active {
  color: var(--accent);
}
</style>
