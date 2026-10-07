/**
 * Cross-page tab visibility.
 *
 * A tab opened from any page other than 翻译 (vocabulary card, history entry,
 * selection hotkey) must become visible immediately. Opening only the tab left
 * the user stranded on the current page — they had to click 翻译 by hand to see
 * the result (improvement feedback).
 */
import { router } from '@/router'

/** Route name of the translate page (router/index.ts). */
export const TRANSLATE_ROUTE_NAME = 'translate'

/** Navigates to the translate page unless it is already the active route. */
export function showTranslatePage(): void {
  if (router.currentRoute.value.name === TRANSLATE_ROUTE_NAME) return
  void router.push({ name: TRANSLATE_ROUTE_NAME })
}
