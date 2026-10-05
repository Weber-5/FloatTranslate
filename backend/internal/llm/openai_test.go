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

func TestOpenAIAdapterCompleteRequestShape(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"word\":\"run\"}"}}]}`))
	}))
	defer srv.Close()

	adapter := NewOpenAIAdapter(AdapterConfig{
		Mode: "deepseek", BaseURL: srv.URL, APIKey: "sk-test-key",
		TranslationModel: "deepseek-flash", ChatModel: "deepseek-flash",
	})
	resp, err := adapter.Complete(context.Background(), CompleteRequest{
		Model:        "deepseek-flash",
		Kind:         KindWord,
		SystemPrompt: "system prompt",
		Prompt:       "user prompt",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("authorization = %q", gotAuth)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("request body is not JSON: %v (%s)", err, gotBody)
	}
	if body["model"] != "deepseek-flash" || body["stream"] != false || body["temperature"] != 0.2 {
		t.Errorf("frozen request fields wrong: %v", body)
	}
	rf, ok := body["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_object" {
		t.Errorf("response_format json_object missing: %v", body["response_format"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	m0, _ := msgs[0].(map[string]any)
	m1, _ := msgs[1].(map[string]any)
	if m0["role"] != "system" || m1["role"] != "user" {
		t.Errorf("message roles wrong: %v %v", m0, m1)
	}
	// Translation must explicitly DISABLE thinking in EVERY dialect we know of
	// (improvement bug #1): `enable_thinking` is only the Qwen/DashScope style,
	// while DeepSeek's own API and vLLM-served hybrids use the other two. A
	// model that keeps thinking ON turns a one-word lookup into a multi-second
	// call.
	if body["enable_thinking"] != false {
		t.Errorf("enable_thinking = %v, want explicit false", body["enable_thinking"])
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Errorf("thinking disable dialect missing/wrong: %v", body["thinking"])
	}
	ctk, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok || ctk["thinking"] != false {
		t.Errorf("chat_template_kwargs disable dialect missing/wrong: %v", body["chat_template_kwargs"])
	}
	// No field that could *enable* or grow reasoning.
	for _, banned := range []string{"reasoning_effort", `"reasoning"`, "include_reasoning"} {
		if strings.Contains(gotBody, banned) {
			t.Errorf("request body must not contain %q: %s", banned, gotBody)
		}
	}
	if resp.Content != `{"word":"run"}` {
		t.Errorf("content = %q", resp.Content)
	}
}

// A strict OpenAI-compatible endpoint may answer 400 for fields it does not
// know. The translation must still succeed: retry once without the optional
// dialects, keeping the explicit enable_thinking=false.
func TestCompleteRetriesWithoutOptionalThinkingDialectsOn400(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unrecognized request argument supplied: thinking"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"word\":\"run\"}"}}]}`))
	}))
	defer srv.Close()

	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	resp, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("complete after fallback: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("provider calls = %d, want 2 (original + fallback)", len(bodies))
	}
	if !strings.Contains(bodies[0], `"chat_template_kwargs"`) {
		t.Errorf("first attempt should carry the dialects: %s", bodies[0])
	}
	if strings.Contains(bodies[1], `"thinking"`) || strings.Contains(bodies[1], `"chat_template_kwargs"`) {
		t.Errorf("fallback attempt must drop the optional dialects: %s", bodies[1])
	}
	if !strings.Contains(bodies[1], `"enable_thinking":false`) {
		t.Errorf("fallback must keep explicit enable_thinking=false: %s", bodies[1])
	}
	if resp.Content != `{"word":"run"}` {
		t.Errorf("content = %q", resp.Content)
	}
}

// Providers that ignore the suppression return the chain of thought in
// reasoning_content; the payload must still be parsed (and the diagnostic must
// not change the result).
func TestCompleteToleratesReasoningContent(t *testing.T) {
	var sawReasoning bool
	var mux http.ServeMux
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		sawReasoning = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"reasoning_content":"thinking hard","content":"{\"word\":\"run\"}"}}]}`))
	})
	srv := httptest.NewServer(&mux)
	defer srv.Close()

	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	resp, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !sawReasoning || resp.Content != `{"word":"run"}` {
		t.Errorf("reasoning_content must not break parsing: %q", resp.Content)
	}
}

func TestOpenAIAdapterStripsMarkdownFences(t *testing.T) {
	content := "\"```json\\n{\\\"word\\\":\\\"run\\\"}\\n```\""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + content + `}}]}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	resp, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != `{"word":"run"}` {
		t.Errorf("content not stripped: %q", resp.Content)
	}
}

func TestOpenAIAdapterDeepseekPresetURLJoin(t *testing.T) {
	// The deepseek preset posts to {base}/chat/completions with the default
	// base URL https://api.deepseek.com.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{
		Mode: ModeDeepseek, BaseURL: srv.URL, APIKey: "k", TranslationModel: "deepseek-flash",
	})
	if _, err := adapter.Complete(context.Background(), CompleteRequest{Model: "deepseek-flash"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if joinURL(DefaultDeepseekBaseURL, "chat/completions") != "https://api.deepseek.com/chat/completions" {
		t.Errorf("deepseek default URL join wrong: %s", joinURL(DefaultDeepseekBaseURL, "chat/completions"))
	}
	if joinURL("https://x.example/v1/", "chat/completions") != "https://x.example/v1/chat/completions" {
		t.Errorf("trailing slash join wrong")
	}
}

func TestOpenAIAdapterHTTP401MapsToConnectionFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "bad"})
	_, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v, want HTTP 401 detail", err)
	}
	if !errors.Is(err, ErrConnection) {
		t.Errorf("401 must map to ErrConnection sentinel, got %v", err)
	}
}

func TestOpenAIAdapter404MapsToConnectionFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	_, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if !errors.Is(err, ErrConnection) {
		t.Errorf("404 must map to ErrConnection, got %v", err)
	}
}

func TestOpenAIAdapterTimeoutMapsToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k", Timeout: 50 * time.Millisecond})
	_, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("timeout must map to ErrUnavailable, got %v", err)
	}
}

func TestOpenAIAdapterConnectionRefusedMapsToConnectionFailed(t *testing.T) {
	// Reserve a port then close the listener so connections are refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: url, APIKey: "k", Timeout: time.Second})
	_, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"})
	if !errors.Is(err, ErrConnection) {
		t.Errorf("connection refused must map to ErrConnection, got %v", err)
	}
}

func TestOpenAIAdapterMalformedEnvelopeMapsToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"nope":true}`))
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	if _, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("missing choices must map to ErrUnavailable, got %v", err)
	}
}

func TestOpenAIAdapter5xxMapsToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	adapter := NewOpenAIAdapter(AdapterConfig{BaseURL: srv.URL, APIKey: "k"})
	if _, err := adapter.Complete(context.Background(), CompleteRequest{Model: "m"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("502 must map to ErrUnavailable, got %v", err)
	}
}

func TestTestConnectionModelsSuccess(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	res := TestConnection(context.Background(), TestConfig{Mode: ModeDeepseek, BaseURL: srv.URL, APIKey: "sk-abc", Model: "m"})
	if !res.OK || res.Message != "连接成功" {
		t.Errorf("result = %+v", res)
	}
	if gotPath != "/models" || gotAuth != "Bearer sk-abc" {
		t.Errorf("probe = %s %s", gotPath, gotAuth)
	}
}

func TestTestConnectionChatFallbackWhenModelsMissing(t *testing.T) {
	var modelSeen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelSeen = append(modelSeen, r.URL.Path)
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Validate the frozen ping body.
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if body["max_tokens"] != float64(1) || body["model"] != "m1" {
			t.Errorf("ping body wrong: %s", raw)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer srv.Close()
	res := TestConnection(context.Background(), TestConfig{Mode: ModeOpenAICompatible, BaseURL: srv.URL, APIKey: "sk-abc", Model: "m1"})
	if !res.OK {
		t.Errorf("fallback should succeed, got %+v (paths %v)", res, modelSeen)
	}
	if len(modelSeen) != 2 || modelSeen[0] != "/models" || modelSeen[1] != "/chat/completions" {
		t.Errorf("probe order wrong: %v", modelSeen)
	}
}

func TestTestConnection401Message(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	res := TestConnection(context.Background(), TestConfig{BaseURL: srv.URL, APIKey: "sk-abc", Model: "m"})
	if res.OK || res.Message != "API Key 无效或未授权" {
		t.Errorf("result = %+v, want 401 mapping", res)
	}
	if strings.Contains(res.Message, "sk-abc") {
		t.Errorf("message must never contain the key")
	}
}

func TestTestConnection404Message(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	res := TestConnection(context.Background(), TestConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if res.OK || res.Message != "端点不存在，请检查 Base URL" {
		t.Errorf("result = %+v", res)
	}
}

func TestTestConnectionTimeoutMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
	}))
	defer srv.Close()
	res := TestConnection(context.Background(), TestConfig{
		BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: 80 * time.Millisecond,
	})
	if res.OK || res.Message != "连接超时" {
		t.Errorf("result = %+v, want 连接超时", res)
	}
}

func TestTestConnectionNoKey(t *testing.T) {
	res := TestConnection(context.Background(), TestConfig{BaseURL: "https://x", Model: "m"})
	if res.OK || !strings.Contains(res.Message, "API Key") {
		t.Errorf("result = %+v", res)
	}
}

func TestTestConnectionOpenAICompatibleRequiresBaseURL(t *testing.T) {
	// Only the deepseek preset has a default base URL; openai_compatible
	// without a base URL fails fast without any request.
	res := TestConnection(context.Background(), TestConfig{Mode: ModeOpenAICompatible, APIKey: "k", Model: "m"})
	if res.OK || res.Message != "Base URL 不能为空" {
		t.Errorf("result = %+v, want Base URL 不能为空", res)
	}
}

func TestStripFences(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":   `{"a":1}`,
		"```\nplain\n```":           "plain",
		"  ```JSON\n{\"a\":1}\n```": `{"a":1}`,
		"{\"a\":1}":                 `{"a":1}`,
	}
	for in, want := range cases {
		if got := stripFences(in); got != want {
			t.Errorf("stripFences(%q) = %q, want %q", in, got, want)
		}
	}
}
