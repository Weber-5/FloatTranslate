/**
 * Chat session export (docs/00 §7 /slash /export): compose Markdown from the
 * active chat and hand it to the Tauri host command `export_markdown`
 * (real save dialog + write). In pure-browser/mock mode — or when the host
 * command is not available yet — fall back to a Blob download with the same
 * file name.
 *
 * Frozen host contract (Phase 4):
 *   export_markdown({ default_file_name: string, content: string })
 *     → { ok: boolean, path?: string, cancelled?: boolean }
 */
import type { Chat, ChatMessage } from '@/api/types'
import { isTauri } from '@/api'

export interface ExportMarkdownResult {
  ok: boolean
  path?: string
  cancelled?: boolean
}

export interface ExportMarkdownArgs {
  default_file_name: string
  content: string
  /** Tauri v2 defaults to camelCase arg keys; send both spellings so the
   * host accepts the frozen snake_case contract regardless of its
   * rename_all setting. Extra keys are ignored by the command extractor. */
  defaultFileName: string
}

type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

let invokeOverride: TauriInvoke | null = null

/**
 * Test-only: replace the Tauri invoke used for export_markdown. Inert outside
 * dev/test builds (UX review 2026-10-07, cleanup decision A).
 */
export function __setExportInvokeForTests(fn: TauriInvoke | null): void {
  if (!import.meta.env.DEV) return
  invokeOverride = fn
}

async function tauriInvoke(cmd: string, args?: Record<string, unknown>): Promise<unknown> {
  if (invokeOverride) return invokeOverride(cmd, args)
  const { invoke } = await import('@tauri-apps/api/core')
  return invoke(cmd, args)
}

/**
 * Composes the exported Markdown: title header + exported_at, then one
 * section per message with i18n role labels. Freeze decision: message
 * content only — reasoning is NOT exported.
 */
export function composeChatMarkdown(
  chat: Chat,
  messages: ChatMessage[],
  roleUser: string,
  roleAssistant: string,
  exportedAtLabel: string,
  exportedAt: string,
): string {
  const lines: string[] = [`# ${chat.title}`, '', `${exportedAtLabel} ${exportedAt}`, '']
  for (const message of messages) {
    const role = message.role === 'user' ? roleUser : roleAssistant
    lines.push(`**${role}**：`, '', message.content, '')
  }
  return lines.join('\n')
}

function downloadBlob(fileName: string, content: string): void {
  const safeName = fileName.replace(/[\\/:*?"<>|]/g, '_').trim() || 'chat.md'
  const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = safeName.endsWith('.md') ? safeName : `${safeName}.md`
  anchor.click()
  URL.revokeObjectURL(url)
}

/**
 * Exports via the Tauri host when available; falls back to a Blob download.
 * Availability detection: the invoke result is inspected with optional
 * chaining — a missing host command rejects (or returns nothing) and we
 * degrade to the browser path instead of failing the export.
 */
export async function exportMarkdownToDisk(
  defaultFileName: string,
  content: string,
): Promise<ExportMarkdownResult> {
  if (isTauri() || invokeOverride) {
    try {
      const args: ExportMarkdownArgs = {
        default_file_name: defaultFileName,
        defaultFileName,
        content,
      }
      const result = (await tauriInvoke('export_markdown', { ...args })) as
        | { ok?: boolean; path?: string; cancelled?: boolean }
        | null
        | undefined
      if (result && typeof result === 'object') {
        if (result.cancelled === true) return { ok: false, cancelled: true }
        if (result.ok === true) {
          return { ok: true, path: typeof result.path === 'string' ? result.path : undefined }
        }
      }
      // Malformed/missing result: treat as host without the command.
    } catch {
      // Command not registered (host not updated yet) → blob fallback.
    }
  }
  downloadBlob(defaultFileName, content)
  return { ok: true, path: undefined }
}
