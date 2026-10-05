package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamTestServer spins an OpenAI-compatible SSE backend from raw lines.
func streamTestServer(t *testing.T, sseBody string, capture func(method, path, auth, body string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if capture != nil {
			capture(r.Method, r.URL.Path, r.Header.Get("Authorization"), string(raw))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, line := range strings.Split(sseBody, "\n") {
			_, _ = io.WriteString(w, line+"\n")
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collectDeltas(t *testing.T, adapter *OpenAIAdapter, req StreamRequest) ([]StreamDelta, error) {
	t.Helper()
	var got []StreamDelta
	err := adapter.Stream(context.Background(), req, func(d StreamDelta) { got = append(got, d) })
	return got, err
}

func TestAdapterStreamParsesReasoningAndContent(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotBody string
	srv := streamTestServer(t, strings.Join([]string{
		`: ping`,
		`data: {"choices":[{"delta":{"reasoning_content":"思考A"}}]}`,
		`data: {"choices":[{"delta":{"content":"答案A"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"思考B","content":"答案B"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n"), func(method, path, auth, body string) {
		gotMethod, gotPath, gotAuth, gotBody = method, path, auth, body
	})
	adapter := NewOpenAIAdapter(AdapterConfig{Mode: ModeDeepseek, BaseURL: srv.URL, APIKey: "sk-stream"})

	deltas, err := collectDeltas(t, adapter, StreamRequest{
		Model:    "chat-model",
		Messages: []ChatMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	// A chunk carrying both fields emits two deltas, reasoning first.
	want := []StreamDelta{{Reasoning: "思考A"}, {Content: "答案A"}, {Reasoning: "思考B"}, {Content: "答案B"}}
	if len(deltas) != len(want) {
		t.Fatalf("deltas = %+v, want %+v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Errorf("deltas[%d] = %+v, want %+v", i, deltas[i], want[i])
		}
	}
	if gotMethod != http.MethodPost || gotPath != "/chat/completions" || gotAuth != "Bearer sk-stream" {
		t.Errorf("request = %s %s %s", gotMethod, gotPath, gotAuth)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("request body invalid: %v", err)
	}
	if body["stream"] != true || body["temperature"] != 0.7 || body["model"] != "chat-model" {
		t.Errorf("frozen stream request fields wrong: %v", body)
	}
	if body["enable_thinking"] != false {
		t.Errorf("enable_thinking must be explicit false when thinking=false: %s", gotBody)
	}
	// Chat keeps the conservative shape on purpose: the extra
	// thinking-suppression dialects are only for translation/compact calls,
	// where no vendor-specific 400 fallback risk is acceptable on the live
	// chat stream.
	if strings.Contains(gotBody, `"thinking"`) || strings.Contains(gotBody, `"chat_template_kwargs"`) {
		t.Errorf("chat stream must not carry the translation-only dialects: %s", gotBody)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages = %d, want 2", len(msgs))
	}
}

func TestAdapterStreamSendsEnableThinkingOnlyWhenRequested(t *testing.T) {
	var gotBody string
	srv := streamTestServer(t, "data: [DONE]\n", func(_, _, _, body string) { gotBody = body })
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	if _, err := collectDeltas(t, adapter, StreamRequest{Model: "m", Thinking: true}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["enable_thinking"] != true {
		t.Errorf("enable_thinking = %v, want true", body["enable_thinking"])
	}
	if _, present := body["response_format"]; present {
		t.Errorf("streaming chat must not request json_object: %s", gotBody)
	}
}

func TestAdapterStreamStopsAtEOFSentinelless(t *testing.T) {
	// Provider closes without [DONE]: treat as a finished stream.
	srv := streamTestServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"尾\"}}]}\n", nil)
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	deltas, err := collectDeltas(t, adapter, StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(deltas) != 1 || deltas[0].Content != "尾" {
		t.Errorf("deltas = %+v", deltas)
	}
}

func TestAdapterStreamHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "bad"})
	_, err := collectDeltas(t, adapter, StreamRequest{Model: "m"})
	if !errors.Is(err, ErrConnection) {
		t.Errorf("401 must map to ErrConnection, got %v", err)
	}

	// Not configured at all: ErrNotConfigured before any request.
	adapter = NewOpenAIAdapter(AdapterConfig{BaseURL: "https://x", APIKey: "  "})
	if _, err := collectDeltas(t, adapter, StreamRequest{Model: "m"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("missing key must map to ErrNotConfigured, got %v", err)
	}
}

func TestAdapterStreamMidStreamErrorField(t *testing.T) {
	srv := streamTestServer(t, strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"前半"}}]}`,
		`data: {"error":{"message":"overloaded"}}`,
		``,
	}, "\n"), nil)
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	deltas, err := collectDeltas(t, adapter, StreamRequest{Model: "m"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("mid-stream error must map to ErrUnavailable, got %v", err)
	}
	if len(deltas) != 1 {
		t.Errorf("deltas = %+v", deltas)
	}
}

func TestAdapterStreamContextCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"开头\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-block // hold the stream open until the test releases it
	}))
	defer func() { close(block); srv.Close() }()

	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	ctx, cancel := context.WithCancel(context.Background())
	var got []StreamDelta
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- adapter.Stream(ctx, StreamRequest{Model: "m"}, func(d StreamDelta) {
			got = append(got, d)
			cancel() // cancel as soon as the first delta arrives
		})
	}()
	select {
	case err := <-streamErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stream error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not return after cancel")
	}
	if len(got) != 1 || got[0].Content != "开头" {
		t.Errorf("deltas = %+v", got)
	}
}

func TestAdapterChatCompletePlain(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  摘要正文  "}}]}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	out, err := adapter.ChatComplete(context.Background(), ChatCompletionRequest{
		Model: "m",
		Messages: []ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "summarize"},
		},
	})
	if err != nil {
		t.Fatalf("chat complete: %v", err)
	}
	if out != "摘要正文" {
		t.Errorf("out = %q, want trimmed 摘要正文", out)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["stream"] != false || body["temperature"] != 0.7 {
		t.Errorf("frozen ChatComplete fields wrong: %v", body)
	}
	if _, present := body["response_format"]; present {
		t.Errorf("plain chat completion must not set response_format: %s", gotBody)
	}
	if body["enable_thinking"] != false {
		t.Errorf("plain chat completion must set enable_thinking=false explicitly: %s", gotBody)
	}

	// Error mapping matches the streaming surface.
	unauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer unauth.Close()
	adapter = NewOpenAIAdapter(AdapterConfig{BaseURL: unauth.URL, APIKey: "k"})
	if _, err := adapter.ChatComplete(context.Background(), ChatCompletionRequest{Model: "m"}); !errors.Is(err, ErrConnection) {
		t.Errorf("403 must map to ErrConnection, got %v", err)
	}
}

// The scanner path must tolerate CRLF line endings from Windows-side proxies.
func TestAdapterStreamToleratesCRLF(t *testing.T) {
	srv := streamTestServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\r\ndata: [DONE]\r\n", nil)
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	deltas, err := collectDeltas(t, adapter, StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(deltas) != 1 || deltas[0].Content != "a" {
		t.Errorf("deltas = %+v", deltas)
	}
}

// Guard the buffered scanner against oversized delta lines.
func TestAdapterStreamLargeLine(t *testing.T) {
	big := strings.Repeat("字", 200_000) // ~600KB utf8 in one SSE line
	payload := `data: {"choices":[{"delta":{"content":"` + big + `"}}]}` + "\ndata: [DONE]\n"
	srv := streamTestServer(t, payload, nil)
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	var size int
	err := adapter.Stream(context.Background(), StreamRequest{Model: "m"}, func(d StreamDelta) { size += len(d.Content) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if size != len(big) {
		t.Errorf("received %d bytes of the big delta, want %d", size, len(big))
	}
}
