// OpenAI Chat Completions adapter shared by both Phase 2 provider modes
// (docs/06 §2): the "deepseek" preset (default base URL
// https://api.deepseek.com) and "openai_compatible" (user base URL). Both
// speak the same OpenAI-style protocol:
//
//	POST {base}/chat/completions
//	Authorization: Bearer <key>
//	{model, messages, stream:false, temperature:0.2,
//	 response_format:{"type":"json_object"}}
//
// Translation requests never carry thinking/reasoning fields (translation
// thinking is always off, docs/06 §11). The choices[0].message.content
// string is returned as the raw JSON payload; markdown fences are stripped.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Provider modes (openapi enum). Both share the OpenAI Chat Completions
// wire protocol implemented by OpenAIAdapter.
const (
	ModeDeepseek         = "deepseek"
	ModeOpenAICompatible = "openai_compatible"
)

// DefaultDeepseekBaseURL is the frozen deepseek preset base URL
// (docs/06 §2); the endpoint path is {base}/chat/completions.
const DefaultDeepseekBaseURL = "https://api.deepseek.com"

// DefaultRequestTimeout bounds a single provider HTTP call.
const DefaultRequestTimeout = 60 * time.Second

// AdapterConfig is the resolved provider configuration for one adapter.
type AdapterConfig struct {
	// Mode is "deepseek" or "openai_compatible" (both use the same wire
	// protocol; the mode only fixes defaults).
	Mode string
	// BaseURL is the provider base URL without trailing slash semantics;
	// the adapter posts to {BaseURL}/chat/completions.
	BaseURL string
	// APIKey is the bearer token; never logged, never persisted.
	APIKey string
	// TranslationModel / ChatModel are the two frozen model slots.
	TranslationModel string
	ChatModel        string
	// Timeout bounds one HTTP call; 0 means DefaultRequestTimeout.
	Timeout time.Duration
	// HTTPClient overrides the transport (used by tests).
	HTTPClient *http.Client
}

// OpenAIAdapter implements Provider against any OpenAI Chat Completions
// compatible endpoint.
type OpenAIAdapter struct {
	cfg    AdapterConfig
	client *http.Client
}

// NewOpenAIAdapter builds an adapter from cfg.
func NewOpenAIAdapter(cfg AdapterConfig) *OpenAIAdapter {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &OpenAIAdapter{cfg: cfg, client: client}
}

// Capabilities reports static chat capabilities. Context/output windows are
// unknown for arbitrary OpenAI-compatible endpoints, so they report 0 and
// the runtime capability view falls back to the configured limits.
func (a *OpenAIAdapter) Capabilities() Capabilities {
	return Capabilities{
		SupportsThinking:         true, // chat only; translation never thinks
		SupportsStructuredOutput: true,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the frozen Phase 2 translation request shape. There is
// deliberately no thinking/reasoning field: translation thinking is always
// off and unknown fields must not be sent to best-effort providers.
type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Stream         bool            `json:"stream"`
	Temperature    float64         `json:"temperature"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete performs one non-streaming Chat Completions call and returns the
// extracted raw JSON payload (markdown fences stripped).
func (a *OpenAIAdapter) Complete(ctx context.Context, req CompleteRequest) (CompleteResponse, error) {
	if strings.TrimSpace(a.cfg.APIKey) == "" {
		return CompleteResponse{}, fmt.Errorf("%w: api key missing", ErrNotConfigured)
	}
	body := chatRequest{
		Model: req.Model,
		Messages: []chatMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.Prompt},
		},
		Stream:         false,
		Temperature:    0.2,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}
	content, err := a.postChat(ctx, body)
	if err != nil {
		return CompleteResponse{}, err
	}
	return CompleteResponse{Content: stripFences(content)}, nil
}

// postChat issues one Chat Completions call and returns
// choices[0].message.content.
func (a *OpenAIAdapter) postChat(ctx context.Context, body chatRequest) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode chat request: %w", err)
	}
	status, respBody, err := doProviderRequest(ctx, a.client, http.MethodPost,
		joinURL(a.cfg.BaseURL, "chat/completions"), a.cfg.APIKey, raw)
	if err != nil {
		return "", classifyTransportError(err)
	}
	if status != http.StatusOK {
		return "", providerHTTPError(status, respBody)
	}
	var decoded chatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return "", fmt.Errorf("%w: response is not valid JSON: %v", ErrUnavailable, err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("%w: response has no choices", ErrUnavailable)
	}
	return decoded.Choices[0].Message.Content, nil
}

// doProviderRequest performs one authenticated provider HTTP call and
// returns the raw transport error (callers classify it).
func doProviderRequest(ctx context.Context, client *http.Client, method, url, apiKey string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// maxProviderResponseBytes bounds how much of a provider response is read.
const maxProviderResponseBytes = 32 << 20

// classifyTransportError maps client/transport failures to the sentinels.
func classifyTransportError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: canceled", ErrUnavailable)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fmt.Errorf("%w: timeout", ErrUnavailable)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: timeout", ErrUnavailable)
	}
	return fmt.Errorf("%w: %v", ErrConnection, err)
}

// providerHTTPError maps a non-200 provider status to the sentinels:
// auth/endpoint problems are connection-class failures, rate limiting and
// server errors are availability problems.
func providerHTTPError(status int, body []byte) error {
	detail := truncateResponse(body)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d: %s", ErrConnection, status, detail)
	case status == http.StatusNotFound:
		return fmt.Errorf("%w: HTTP 404 (endpoint not found, check base URL)", ErrConnection)
	case status == http.StatusTooManyRequests || status >= 500:
		return fmt.Errorf("%w: HTTP %d: %s", ErrUnavailable, status, detail)
	default:
		return fmt.Errorf("%w: HTTP %d: %s", ErrUnavailable, status, detail)
	}
}

func truncateResponse(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// joinURL joins a base URL and an endpoint path.
func joinURL(base, path string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/" + strings.TrimLeft(path, "/")
}

var fencePattern = regexp.MustCompile("(?s)^```[A-Za-z0-9_-]*[ \\t]*\\r?\\n?(.*)\\r?\\n?```[ \\t\\r\\n]*$")

// stripFences removes a wrapping markdown code fence from a payload, per the
// frozen extraction rule (the model may wrap JSON in ```json fences even
// though the prompt forbids it).
func stripFences(content string) string {
	s := strings.TrimSpace(content)
	if m := fencePattern.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

// TestConfig is the provider configuration used by a connection test.
type TestConfig struct {
	Mode    string
	BaseURL string
	APIKey  string
	// Model is the translation model used for the chat-completions fallback
	// probe.
	Model string
	// Timeout bounds each probe; 0 means DefaultRequestTimeout.
	Timeout time.Duration
	// HTTPClient overrides the transport (used by tests).
	HTTPClient *http.Client
}

// TestResult is the outcome of a connection test. Message is user-facing
// (Chinese), never contains the API key, and mirrors the Phase 2 freeze:
// 401 → "API Key 无效或未授权", 404 → "端点不存在，请检查 Base URL",
// timeouts → "连接超时", etc.
type TestResult struct {
	OK      bool
	Message string
}

// TestConnection probes the provider endpoint: first GET {base}/models with
// the key; if that does not succeed it falls back to POST {base}/chat/
// completions with {model, max_tokens:1, messages:[{role:"user",
// content:"ping"}]}. Both probes use the same key and never echo it.
func TestConnection(ctx context.Context, cfg TestConfig) TestResult {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return TestResult{OK: false, Message: "请先填写 API Key"}
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		if cfg.Mode == ModeOpenAICompatible || cfg.Mode == "" {
			return TestResult{OK: false, Message: "Base URL 不能为空"}
		}
		base = DefaultDeepseekBaseURL
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultTranslationModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	// Probe 1: GET {base}/models.
	status, _, err := doProviderRequest(ctx, client, http.MethodGet, joinURL(base, "models"), cfg.APIKey, nil)
	if err == nil && status >= 200 && status < 300 {
		return TestResult{OK: true, Message: "连接成功"}
	}

	// Probe 2 (fallback): POST {base}/chat/completions with a 1-token ping.
	maxTokens := 1
	ping := chatRequest{
		Model:     cfg.Model,
		Messages:  []chatMessage{{Role: "user", Content: "ping"}},
		Stream:    false,
		MaxTokens: &maxTokens,
	}
	raw, merr := json.Marshal(ping)
	if merr != nil {
		return TestResult{OK: false, Message: "测试请求构造失败"}
	}
	status2, body2, err2 := doProviderRequest(ctx, client, http.MethodPost, joinURL(base, "chat/completions"), cfg.APIKey, raw)
	if err2 == nil && status2 >= 200 && status2 < 300 {
		return TestResult{OK: true, Message: "连接成功"}
	}

	// Both probes failed: report the more informative (chat) failure.
	if err2 != nil {
		return TestResult{OK: false, Message: describeTransportError(err2)}
	}
	return TestResult{OK: false, Message: describeHTTPFailure(status2, body2)}
}

// DefaultTranslationModel is the default model string used by the ping
// probe when no model is configured.
const DefaultTranslationModel = "deepseek-flash"

// describeHTTPFailure maps a failed provider HTTP status to a concise
// Chinese message (frozen Phase 2 mapping).
func describeHTTPFailure(status int, body []byte) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "API Key 无效或未授权"
	case http.StatusNotFound:
		return "端点不存在，请检查 Base URL"
	case http.StatusTooManyRequests:
		return "请求过于频繁，请稍后重试"
	}
	if status >= 500 {
		return fmt.Sprintf("服务端错误（HTTP %d），请稍后重试", status)
	}
	return fmt.Sprintf("请求失败（HTTP %d），请检查 Provider 配置", status)
}

// describeTransportError maps a transport failure to a concise Chinese
// message; timeouts get their own frozen wording.
func describeTransportError(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "连接超时"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "连接超时"
	}
	if errors.Is(err, context.Canceled) {
		return "测试已取消"
	}
	return "无法连接到服务，请检查网络或 Base URL"
}
