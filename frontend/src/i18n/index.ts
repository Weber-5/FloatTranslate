/**
 * vue-i18n instance. zh-CN is the default and complete locale; en mirrors it.
 */
import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'
import en from './en'

export type MessageSchema = typeof zhCN

export const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'en',
  messages: {
    'zh-CN': zhCN,
    en,
  },
})
