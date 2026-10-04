/**
 * Client-side proxy URL validation (Phase 5 contract): the URL scheme must
 * match the selected proxy mode. `system`/`none` never carry a URL.
 */
import type { ProxyMode } from '@/api/types'

/** Scheme each custom mode requires, e.g. http → "http:". Null for system/none. */
export function requiredProxyScheme(mode: ProxyMode): string | null {
  if (mode === 'system' || mode === 'none') return null
  return `${mode}:`
}

/**
 * True when `url` parses as a URL whose protocol matches the mode, e.g.
 * http://127.0.0.1:8080, https://proxy.corp:8443, socks5://user@host:1080.
 * Non-special schemes (socks5) are parsed by the WHATWG URL parser too.
 */
export function isValidProxyUrl(mode: ProxyMode, url: string): boolean {
  const scheme = requiredProxyScheme(mode)
  if (scheme === null) return true
  const trimmed = url.trim()
  if (trimmed.length === 0) return false
  try {
    return new URL(trimmed).protocol === scheme
  } catch {
    return false
  }
}
