/**
 * Application-wide constants shared by stores, mock client and settings UI.
 */
export const APP_VERSION = '1.0.2'

/** Frozen hotkey defaults (docs/00 §3), mirrored by the Go settings store. */
export const DEFAULT_HOTKEY_TOGGLE = 'Ctrl+Alt+Space'
export const DEFAULT_HOTKEY_QUICK_TRANSLATE = 'Ctrl+Alt+Q'

export const DEFAULT_AI_SYSTEM_PROMPT =
  '你是 FloatTranslate 的内置 AI 助手，帮助用户理解英文内容。' +
  '用简体中文回答，保持简洁、准确、友好；解释语法或概念时给出清晰的结构。' +
  '用户没有提供背景时，先简要说明再展开。'
