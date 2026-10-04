/**
 * TypeScript mirrors of the schemas in openapi/openapi.yaml (v1.0.0).
 * Hand-written to match the contract EXACTLY — field names stay snake_case.
 * Do not rename fields here without changing openapi.yaml.
 */

// ---- Standard error model (docs/04 §2) --------------------------------

export interface ErrorDetails {
  [key: string]: unknown
}

export interface ErrorBody {
  code: string
  message: string
  retryable: boolean
  details?: ErrorDetails
}

export interface ErrorResponse {
  error: ErrorBody
}

// ---- Runtime / capabilities --------------------------------------------

export interface RuntimeCapabilities {
  configured_context_tokens?: number
  effective_context_tokens?: number
  configured_output_tokens?: number
  effective_output_tokens?: number
  supports_thinking?: boolean
  supports_structured_output?: boolean
}

// ---- Settings -----------------------------------------------------------

/**
 * Non-secret settings. openapi.yaml declares AppSettings as
 * `additionalProperties: true`; this concrete shape follows docs/00 §9 and
 * docs/03 §10 and stays open for extra keys.
 */
export type ThemeMode = 'system' | 'light' | 'dark'
export type ProxyMode = 'system' | 'http' | 'https' | 'socks5'

export interface AppSettings {
  theme?: ThemeMode
  always_on_top?: boolean
  auto_start?: boolean
  hotkey_toggle_window?: string
  hotkey_quick_translate?: string
  context_tokens?: number
  output_tokens?: number
  auto_compact?: boolean
  compact_threshold?: number
  ai_system_prompt?: string
  custom_translation_prompt?: string
  proxy_mode?: ProxyMode
  proxy_host?: string
  proxy_port?: number
  proxy_username?: string
  proxy_password?: string
  [key: string]: unknown
}

export type ProviderMode = 'deepseek' | 'openai_compatible'

export interface ProviderSettingsView {
  mode: ProviderMode
  base_url: string
  translation_model: string
  chat_model: string
  api_key_configured: boolean
  api_key_hint?: string
}

export interface ProviderSettingsUpdate {
  mode: ProviderMode
  base_url: string
  translation_model: string
  chat_model: string
  /** writeOnly — frontend can send it, never receives it back. */
  api_key?: string
}

export interface ProviderTestResult {
  ok: boolean
  message?: string
}

// ---- Translation ----------------------------------------------------------

export type TranslationKind = 'word' | 'text'
export type TranslationSource = 'model' | 'cache'

export interface TranslationRequest {
  text: string
  force_kind?: TranslationKind
  bypass_cache?: boolean
}

export interface WordPosGroup {
  part: string
  meanings: string[]
}

/** Mirrors schemas/word_translation.schema.json. */
export interface WordTranslation {
  word: string
  lemma: string
  phonetic_uk: string
  phonetic_us: string
  parts_of_speech: WordPosGroup[]
  synonyms: string[]
  inflections: string[]
}

export interface TextSegment {
  source: string
  translation: string
}

export interface TextTranslation {
  source_markdown: string
  translated_markdown: string
  segments: TextSegment[]
}

export type TranslationResult = WordTranslation | TextTranslation

export interface TranslationResponse {
  translation_id: string
  kind: TranslationKind
  source: TranslationSource
  result: TranslationResult
  model: string
  created_at: string
}

// ---- History ---------------------------------------------------------------

export interface HistoryItem {
  id: string
  kind: TranslationKind
  input_text: string
  result: TranslationResult
  created_at: string
}

export interface HistoryPage {
  items: HistoryItem[]
  next_cursor?: string | null
}

export interface HistoryQuery {
  query?: string
  kind?: TranslationKind
  limit?: number
  cursor?: string
}

// ---- Vocabulary ---------------------------------------------------------------

export interface VocabularyItem {
  lemma: string
  word: WordTranslation
  saved_at: string
  last_viewed_at: string
}

// ---- Terminology ----------------------------------------------------------------

export interface TerminologyMutation {
  source: string
  target: string
}

export interface TerminologyItem extends TerminologyMutation {
  id: string
}

// ---- Tabs --------------------------------------------------------------------------

export interface TabPayload {
  translation_id?: string
  /** The queried word for word-kind tabs. */
  word?: string
  /** The queried text for text-kind tabs. */
  text?: string
  /** Locally stored structured content (e.g. opened from vocabulary). */
  word_data?: WordTranslation
  [key: string]: unknown
}

export interface TabState {
  id: string
  kind: TranslationKind
  title: string
  position: number
  is_active: boolean
  payload: TabPayload
}

// ---- Chats -----------------------------------------------------------------------------

export interface Chat {
  id: string
  title: string
  created_at: string
  updated_at: string
}

export type ChatRole = 'user' | 'assistant'

export interface ChatMessage {
  id: string
  role: ChatRole
  content: string
  reasoning_content?: string
  created_at: string
}

export interface ChatUpdate {
  title?: string
}

export interface ChatGenerationRequest {
  content: string
  thinking?: boolean
  reference_text?: string
}

// ---- Context ------------------------------------------------------------------------------

export interface ContextValue {
  content: string
}

// ---- Backup / data reset -----------------------------------------------------------------------

export interface FilePathRequest {
  path: string
}
