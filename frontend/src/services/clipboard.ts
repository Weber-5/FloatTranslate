/**
 * Clipboard helper: navigator.clipboard when available, with a hidden
 * textarea fallback for older WebView2 builds. Returns whether the copy
 * succeeded so buttons can show feedback.
 */
export async function copyText(text: string): Promise<boolean> {
  if (text.length === 0) return false
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Fall through to the legacy path.
  }
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', '')
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch {
    return false
  }
}
