// Streaming chat completions for the OpenAI-compatible adapter (Phase 4,
// docs/06 §9-11):
//
//	POST {base}/chat/completions
//	{model, messages, stream:true, temperature:0.7
//	 [, enable_thinking:true when thinking was requested]}
//
// The provider response is OpenAI SSE: lines starting with "data: " carrying
// incremental choice deltas — choices[0].delta.content becomes a content
// fragment and choices[0].delta.reasoning_content a reasoning fragment;
// "data: [DONE]" terminates the stream. Cancellation flows through the
// request context; the returned error then satisfies
// errors.Is(err, context.Canceled).
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// streamClient is shared by streaming calls when the adapter was not built
// with an injected HTTP client. It deliberately has no overall Timeout: a
// chat stream may legitimately stay idle for a long time and cancellation is
// governed entirely by the request context (heartbeats keep it alive).
var (
	streamClientOnce sync.Once
	streamClient     *http.Client
)

func sharedStreamClient() *http.Client {
	streamClientOnce.Do(func() { streamClient = &http.Client{} })
	return streamClient
}

// streamChunk is one parsed provider SSE data payload.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Stream implements Provider. See the interface contract in provider.go.
func (a *OpenAIAdapter) Stream(ctx context.Context, req StreamRequest, onDelta func(StreamDelta)) error {
	if strings.TrimSpace(a.cfg.APIKey) == "" {
		return fmt.Errorf("%w: api key missing", ErrNotConfigured)
	}
	if onDelta == nil {
		onDelta = func(StreamDelta) {}
	}
	body := chatRequest{
		Model:       req.Model,
		Messages:    toWireChatMessages(req.Messages),
		Stream:      true,
		Temperature: 0.7,
	}
	if req.Thinking {
		enable := true
		body.EnableThinking = &enable
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode stream request: %w", err)
	}

	client := a.client
	if a.cfg.HTTPClient == nil {
		client = sharedStreamClient()
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		joinURL(a.cfg.BaseURL, "chat/completions"), strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(httpReq)
	if err != nil {
		return classifyTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
		return providerHTTPError(resp.StatusCode, respBody)
	}

	return consumeProviderStream(ctx, resp.Body, onDelta)
}

// consumeProviderStream parses the OpenAI SSE byte stream, invoking onDelta
// per reasoning/content fragment. The stream ends at "data: [DONE]", at EOF
// or on error; a canceled context always surfaces as context.Canceled.
func consumeProviderStream(ctx context.Context, r io.Reader, onDelta func(StreamDelta)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // SSE comments (" : ping"), "event:" lines, blanks
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			return nil
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // tolerate keep-alive noise; providers send JSON data lines
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return fmt.Errorf("%w: %s", ErrUnavailable, chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.ReasoningContent != "" {
			onDelta(StreamDelta{Reasoning: delta.ReasoningContent})
		}
		if delta.Content != "" {
			onDelta(StreamDelta{Content: delta.Content})
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		if isCanceled(err, ctx) {
			return ctx.Err()
		}
		return classifyTransportError(err)
	}
	// EOF without [DONE]: treat the provider stream as finished.
	return nil
}

// isCanceled reports whether err is (or wraps) the context cancellation of ctx.
func isCanceled(err error, ctx context.Context) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// ChatComplete implements PlainCompleter: one non-streaming plain-text chat
// completion (no response_format — the answer is free text, e.g. a compact
// summary), frozen temperature 0.7 like the streaming chat call.
func (a *OpenAIAdapter) ChatComplete(ctx context.Context, req ChatCompletionRequest) (string, error) {
	if strings.TrimSpace(a.cfg.APIKey) == "" {
		return "", fmt.Errorf("%w: api key missing", ErrNotConfigured)
	}
	body := chatRequest{
		Model:       req.Model,
		Messages:    toWireChatMessages(req.Messages),
		Stream:      false,
		Temperature: 0.7,
	}
	content, err := a.postChat(ctx, body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}
