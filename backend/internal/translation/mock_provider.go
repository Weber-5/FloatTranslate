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
// schemas and supports injected failures/invalid payloads for tests.
type MockProvider struct {
	mu               sync.Mutex
	calls            int
	failures         int // remaining injected connection failures
	invalidResponses int // remaining injected schema-invalid responses
}

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

// mockText builds a TextTranslation payload for input.
func mockText(input string) (string, error) {
	src := input
	payload := dto.TextTranslation{
		SourceMarkdown:     src,
		TranslatedMarkdown: "（mock 译文）\n\n" + src,
		Segments:           []dto.Segment{{Source: src, Translation: "（mock 译文）" + src}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
