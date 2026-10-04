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

// ChatMessage is one provider-protocol chat message (Phase 4 chat pipeline:
// system/user/assistant roles, plain-text content).
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamRequest is a streaming chat completion request (Phase 4 chat
// generation, docs/06 §9-11).
type StreamRequest struct {
	// Model is the chat model string to use.
	Model string
	// Messages is the fully composed provider message list (system context
	// messages first, then the conversation, docs/06 §9 order).
	Messages []ChatMessage
	// Thinking requests the provider's reasoning mode; when true the adapter
	// sends the enable_thinking control field (never for translation).
	Thinking bool
}

// StreamDelta is one parsed provider stream event. At most one of Reasoning
// and Content is non-empty per delta.
type StreamDelta struct {
	// Reasoning is a reasoning/thinking text fragment (SSE reasoning.delta).
	Reasoning string
	// Content is an answer content fragment (SSE content.delta).
	Content string
}

// Provider abstracts an LLM backend.
type Provider interface {
	// Complete performs a non-streaming completion and returns the raw JSON
	// payload string. Errors map to the Err* sentinels above.
	Complete(ctx context.Context, req CompleteRequest) (CompleteResponse, error)
	// Stream runs a streaming chat completion, invoking onDelta once per
	// reasoning/content fragment in provider order. It returns nil after the
	// provider finishes its stream; errors map to the Err* sentinels. When
	// ctx is canceled the call aborts and the returned error satisfies
	// errors.Is(err, context.Canceled) — the chat layer treats that as a
	// user-initiated cancel, not a provider failure.
	Stream(ctx context.Context, req StreamRequest, onDelta func(StreamDelta)) error
	// Capabilities reports provider/model capabilities.
	Capabilities() Capabilities
}

// ChatCompletionRequest is a plain (non-structured, non-streaming) chat
// completion request — used by the chat compact flow (docs/06 §10).
type ChatCompletionRequest struct {
	// Model is the chat model string to use.
	Model string
	// Messages is the full message list (no response_format is ever applied;
	// the answer is plain text).
	Messages []ChatMessage
}

// PlainCompleter is the optional Provider extension for plain-text chat
// completions. The OpenAI-compatible adapter implements it; providers that
// only implement Complete fall back to a JSON-summary completion in the chat
// service.
type PlainCompleter interface {
	// ChatComplete returns the model's plain-text answer (whitespace-trimmed).
	ChatComplete(ctx context.Context, req ChatCompletionRequest) (string, error)
}
