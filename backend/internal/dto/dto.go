// Package dto defines the wire types mirroring openapi/openapi.yaml exactly.
// Field names are snake_case as fixed by the API contract.
package dto

// WordTranslation mirrors components.schemas.WordTranslation and
// schemas/word_translation.schema.json. Keep the field set exactly in sync:
// the mock provider output must validate against the schema.
type WordTranslation struct {
	Word          string         `json:"word"`
	Lemma         string         `json:"lemma"`
	PhoneticUK    string         `json:"phonetic_uk"`
	PhoneticUS    string         `json:"phonetic_us"`
	PartsOfSpeech []PartOfSpeech `json:"parts_of_speech"`
	Synonyms      []string       `json:"synonyms"`
	Inflections   []string       `json:"inflections"`
}

// PartOfSpeech is one entry of WordTranslation.parts_of_speech.
type PartOfSpeech struct {
	Part     string   `json:"part"`
	Meanings []string `json:"meanings"`
}

// TextTranslation mirrors components.schemas.TextTranslation and
// schemas/text_translation.schema.json.
type TextTranslation struct {
	SourceMarkdown     string    `json:"source_markdown"`
	TranslatedMarkdown string    `json:"translated_markdown"`
	Segments           []Segment `json:"segments"`
}

// Segment is one paragraph pair of TextTranslation.segments.
type Segment struct {
	Source      string `json:"source"`
	Translation string `json:"translation"`
}

// TranslationRequest mirrors components.schemas.TranslationRequest.
type TranslationRequest struct {
	Text        string `json:"text"`
	ForceKind   string `json:"force_kind,omitempty"`
	BypassCache bool   `json:"bypass_cache,omitempty"`
}

// TranslationResponse mirrors components.schemas.TranslationResponse. Result
// holds the schema-validated WordTranslation or TextTranslation (decoded
// JSON), depending on Kind.
type TranslationResponse struct {
	TranslationID string `json:"translation_id"`
	Kind          string `json:"kind"`
	Source        string `json:"source"` // model | cache
	Result        any    `json:"result"`
	Model         string `json:"model"`
	CreatedAt     string `json:"created_at"` // RFC3339 UTC
}

// HistoryItem mirrors components.schemas.HistoryItem. Source and Model are
// optional per the contract but always populated by the server.
type HistoryItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	InputText string `json:"input_text"`
	Result    any    `json:"result"`
	Source    string `json:"source,omitempty"` // model | cache
	Model     string `json:"model,omitempty"`
	CreatedAt string `json:"created_at"`
}

// HistoryPage is the GET /history response envelope.
type HistoryPage struct {
	Items      []HistoryItem `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

// VocabularyItem mirrors components.schemas.VocabularyItem.
type VocabularyItem struct {
	Lemma        string           `json:"lemma"`
	Word         *WordTranslation `json:"word"`
	SavedAt      string           `json:"saved_at"`
	LastViewedAt string           `json:"last_viewed_at"`
}

// TerminologyMutation mirrors components.schemas.TerminologyMutation.
type TerminologyMutation struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// TerminologyItem mirrors components.schemas.TerminologyItem.
type TerminologyItem struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// TabState mirrors components.schemas.TabState.
type TabState struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Position int            `json:"position"`
	IsActive bool           `json:"is_active"`
	Payload  map[string]any `json:"payload"`
}

// ProviderSettingsView mirrors components.schemas.ProviderSettingsView.
type ProviderSettingsView struct {
	Mode             string `json:"mode"` // deepseek | openai_compatible
	BaseURL          string `json:"base_url"`
	TranslationModel string `json:"translation_model"`
	ChatModel        string `json:"chat_model"`
	APIKeyConfigured bool   `json:"api_key_configured"`
	APIKeyHint       string `json:"api_key_hint"`
}

// ProviderSettingsUpdate mirrors components.schemas.ProviderSettingsUpdate.
// APIKey is write-only: it is never returned by any GET endpoint.
type ProviderSettingsUpdate struct {
	Mode             string `json:"mode"`
	BaseURL          string `json:"base_url"`
	TranslationModel string `json:"translation_model"`
	ChatModel        string `json:"chat_model"`
	APIKey           string `json:"api_key,omitempty"`
}

// TestConnectionResponse is the POST /settings/provider/test body.
type TestConnectionResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// RuntimeCapabilities mirrors components.schemas.RuntimeCapabilities.
type RuntimeCapabilities struct {
	ConfiguredContextTokens  int  `json:"configured_context_tokens"`
	EffectiveContextTokens   int  `json:"effective_context_tokens"`
	ConfiguredOutputTokens   int  `json:"configured_output_tokens"`
	EffectiveOutputTokens    int  `json:"effective_output_tokens"`
	SupportsThinking         bool `json:"supports_thinking"`
	SupportsStructuredOutput bool `json:"supports_structured_output"`
}

// HealthResponse is the GET /health body.
type HealthResponse struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	DBStatus string `json:"db_status"`
}

// --- Phase 4: AI sidebar (chats / generations / contexts) ---

// Chat mirrors components.schemas.Chat.
type Chat struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ChatMessage mirrors components.schemas.ChatMessage.
type ChatMessage struct {
	ID               string `json:"id"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	CreatedAt        string `json:"created_at"`
}

// ChatGenerationRequest mirrors components.schemas.ChatGenerationRequest.
type ChatGenerationRequest struct {
	Content       string `json:"content"`
	Thinking      bool   `json:"thinking"`
	ReferenceText string `json:"reference_text,omitempty"`
}

// ContextValue mirrors components.schemas.ContextValue.
type ContextValue struct {
	Content string `json:"content"`
}

// CompactResponse is the POST /chats/{id}/compact body.
type CompactResponse struct {
	Summary string `json:"summary"`
}

// --- Phase 5: backup / clear / reset ---

// FilePathRequest mirrors components.schemas.FilePathRequest (backup
// export/import bodies).
type FilePathRequest struct {
	Path string `json:"path"`
}

// ExportResponse is the POST /backup/export body.
type ExportResponse struct {
	Exported bool   `json:"exported"`
	Path     string `json:"path"`
}

// ImportCounts reports how many rows each merge rule applied (docs/05 §6).
// MessagesSkipped counts backup messages whose chat_id did not exist after
// the chat merge.
type ImportCounts struct {
	Settings           int `json:"settings"`
	TranslationHistory int `json:"translation_history"`
	Vocabulary         int `json:"vocabulary"`
	Terminology        int `json:"terminology"`
	Chats              int `json:"chats"`
	Messages           int `json:"messages"`
	MessagesSkipped    int `json:"messages_skipped"`
	OpenTabs           int `json:"open_tabs"`
}

// ImportResponse is the POST /backup/import body.
type ImportResponse struct {
	Imported ImportCounts `json:"imported"`
}
