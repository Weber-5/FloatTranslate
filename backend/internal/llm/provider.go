// Package llm defines the provider abstraction (docs/06 §1). The business
// layer depends only on this interface; vendor-specific adapters (DeepSeek
// preset, OpenAI-compatible) implement it. Phase 1 ships a deterministic
// mock implementation.
package llm

import (
	"context"
	"errors"
)

// Sentinel provider errors. The translation pipeline maps them to the
// standard error codes:
//
//	ErrNotConfigured → PROVIDER_NOT_CONFIGURED (retryable=false)
//	ErrConnection    → PROVIDER_CONNECTION_FAILED (retryable=true)
//	ErrUnavailable   → PROVIDER_UNAVAILABLE (retryable=true)
var (
	ErrNotConfigured = errors.New("llm: provider not configured")
	ErrConnection    = errors.New("llm: provider connection failed")
	ErrUnavailable   = errors.New("llm: provider unavailable")
)

// Kind values for translation requests.
const (
	KindWord = "word"
	KindText = "text"
)

// CompleteRequest is a structured-output completion request. The provider
// deals in raw JSON strings: Prompt is the composed user prompt and
// SchemaJSON is the JSON Schema the response payload must satisfy.
type CompleteRequest struct {
	// Model is the translation/chat model string to use.
	Model string
	// Kind is "word" or "text" for translation requests.
	Kind string
	// Input is the (terminology-applied, normalized) user input the prompt
	// was built from. Lets simple adapters echo the input deterministically.
	Input string
	// SystemPrompt is the composed system prompt.
	SystemPrompt string
	// Prompt is the composed user prompt.
	Prompt string
	// SchemaJSON is the JSON Schema for the structured output payload.
	SchemaJSON string
	// RepairOf, when non-empty, marks this request as the single repair
	// attempt for the invalid payload it carries.
	RepairOf string
}

// CompleteResponse carries the provider's raw JSON payload string.
type CompleteResponse struct {
	// Content is the raw JSON payload returned by the model.
	Content string
}

// Capabilities describes what the current provider/model supports.
type Capabilities struct {
	// ContextTokens is the provider's maximum context window.
	ContextTokens int
	// OutputTokens is the provider's maximum output size.
	OutputTokens int
	// SupportsThinking reports reasoning/thinking toggle support (chat only).
	SupportsThinking bool
	// SupportsStructuredOutput reports JSON-schema structured output support.
	SupportsStructuredOutput bool
}

// Provider abstracts an LLM backend. Stream, TestConnection and the chat
// pipeline join in later phases; Phase 1 only requires Complete.
type Provider interface {
	// Complete performs a non-streaming completion and returns the raw JSON
	// payload string. Errors map to the Err* sentinels above.
	Complete(ctx context.Context, req CompleteRequest) (CompleteResponse, error)
	// Capabilities reports provider/model capabilities.
	Capabilities() Capabilities
}
