package translation

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/nlp"
)

// MockProvider is a deterministic in-process llm.Provider used for the
// Phase 1 skeleton. It always produces JSON that passes the embedded
// schemas and supports injected failures/invalid payloads for tests. Since
// Phase 4 it also implements llm.Stream and llm.PlainCompleter with a
// scriptable fake stream so chat generation tests can drive cancel/error/
// blocking scenarios.
type MockProvider struct {
	mu               sync.Mutex
	calls            int
	failures         int // remaining injected connection failures (Complete)
	invalidResponses int // remaining injected schema-invalid responses

	streamCalls    int
	streamFailures int // remaining injected connection failures (Stream)
	chatCalls      int
	chatFailures   int // remaining injected failures (ChatComplete)
	streamFn       streamFunc
}

// streamFunc is a fully scripted Stream replacement for tests.
type streamFunc func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error

// NewMockProvider builds a fresh mock provider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// Capabilities matches the Phase 1 runtime capabilities contract.
func (m *MockProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		ContextTokens:            1_000_000,
		OutputTokens:             8192,
		SupportsThinking:         true,
		SupportsStructuredOutput: true,
	}
}

// InjectFailures makes the next n Complete calls return llm.ErrConnection.
func (m *MockProvider) InjectFailures(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = n
}

// InjectInvalidResponses makes the next n Complete calls return JSON that
// decodes but violates the requested schema (drives the repair flow).
func (m *MockProvider) InjectInvalidResponses(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidResponses = n
}

// Calls reports how many Complete calls were made (cache-hit tests assert
// the provider was NOT called).
func (m *MockProvider) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// StreamCalls reports how many Stream calls were made.
func (m *MockProvider) StreamCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamCalls
}

// ChatCompleteCalls reports how many ChatComplete (compact) calls were made.
func (m *MockProvider) ChatCompleteCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chatCalls
}

// InjectStreamFailures makes the next n Stream calls return llm.ErrConnection.
func (m *MockProvider) InjectStreamFailures(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamFailures = n
}

// InjectChatCompleteFailures makes the next n ChatComplete calls return
// llm.ErrUnavailable (drives the "compact failure never fails a finished
// generation" scenario).
func (m *MockProvider) InjectChatCompleteFailures(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatFailures = n
}

// SetStreamFunc replaces the default Stream behavior with fn for tests
// (pass nil to restore the deterministic default).
func (m *MockProvider) SetStreamFunc(fn func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamFn = fn
}

// Stream implements llm.Provider: with no script it emits one reasoning
// delta followed by deterministic content deltas echoing the last user
// message; a scripted streamFn (SetStreamFunc) takes precedence.
func (m *MockProvider) Stream(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
	m.mu.Lock()
	m.streamCalls++
	if m.streamFailures > 0 {
		m.streamFailures--
		m.mu.Unlock()
		return llm.ErrConnection
	}
	fn := m.streamFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, req, onDelta)
	}

	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = req.Messages[i].Content
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	onDelta(llm.StreamDelta{Reasoning: "（mock 思考过程）"})
	onDelta(llm.StreamDelta{Content: "（mock 回复）"})
	if last != "" {
		onDelta(llm.StreamDelta{Content: last})
	}
	return nil
}

// ChatComplete implements llm.PlainCompleter with deterministic output
// (the compact summary stub).
func (m *MockProvider) ChatComplete(ctx context.Context, req llm.ChatCompletionRequest) (string, error) {
	m.mu.Lock()
	m.chatCalls++
	if m.chatFailures > 0 {
		m.chatFailures--
		m.mu.Unlock()
		return "", llm.ErrUnavailable
	}
	m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "（mock 摘要）本摘要涵盖关键事实、定义、已做的决策、未完成事项与用户偏好。", nil
}

// Complete produces deterministic mock output.
func (m *MockProvider) Complete(ctx context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	m.mu.Lock()
	m.calls++
	if m.failures > 0 {
		m.failures--
		m.mu.Unlock()
		return llm.CompleteResponse{}, llm.ErrConnection
	}
	invalid := m.invalidResponses > 0
	if invalid {
		m.invalidResponses--
	}
	m.mu.Unlock()

	if invalid {
		// Valid JSON that violates every requested schema.
		return llm.CompleteResponse{Content: `{"unexpected":"payload"}`}, nil
	}

	var content string
	switch req.Kind {
	case llm.KindWord:
		word, err := mockWord(req.Input)
		if err != nil {
			return llm.CompleteResponse{}, llm.ErrUnavailable
		}
		content = word
	case llm.KindText:
		text, err := mockText(req.Input)
		if err != nil {
			return llm.CompleteResponse{}, llm.ErrUnavailable
		}
		content = text
	default:
		return llm.CompleteResponse{}, llm.ErrUnavailable
	}
	return llm.CompleteResponse{Content: content}, nil
}

// mockWord builds a WordTranslation payload for input.
func mockWord(input string) (string, error) {
	word := strings.TrimSpace(input)
	if word == "" {
		word = "word"
	}
	lemma := nlp.Lemmatize(word)
	payload := dto.WordTranslation{
		Word:       word,
		Lemma:      lemma,
		PhoneticUK: "/ˈmɒk/",
		PhoneticUS: "/ˈmɑːk/",
		PartsOfSpeech: []dto.PartOfSpeech{
			{Part: "noun", Meanings: []string{"（mock 释义）" + lemma + " 的名词释义"}},
			{Part: "verb", Meanings: []string{"（mock 释义）" + lemma + " 的动词释义"}},
		},
		Synonyms:    []string{"sample", "example"},
		Inflections: []string{lemma + "s", lemma + "ed", lemma + "ing"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// mockText builds a schema-valid per-chunk text payload for input
// (Phase 3 contract: {"translated_markdown", "segments"} — the pipeline
// assembles source_markdown from the original input itself).
func mockText(input string) (string, error) {
	payload := map[string]any{
		"translated_markdown": "（mock 译文）\n\n" + input,
		"segments":            []dto.Segment{{Source: input, Translation: "（mock 译文）" + input}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
