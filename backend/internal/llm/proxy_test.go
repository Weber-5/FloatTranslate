package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/proxy"
)

// TestAdapterHonorsProxySettings freezes the Phase 5 proxy contract for
// provider clients: with an explicit http proxy configured, Complete and
// Stream traffic is sent to the proxy endpoint and never directly to the
// origin base URL.
func TestAdapterHonorsProxySettings(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "chat/completions") {
			// Complete posts stream:false; Stream posts stream:true.
			buf, _ := io.ReadAll(r.Body)
			if strings.Contains(string(buf), `"stream":true`) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"via-proxy\"}}]}\n\ndata: [DONE]\n\n"))
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"via\":\"proxy\"}"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer proxySrv.Close()

	originHits := 0
	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer originSrv.Close()

	cfg := AdapterConfig{
		BaseURL: originSrv.URL,
		APIKey:  "k",
		Proxy:   proxy.Settings{Mode: proxy.ModeHTTP, URL: proxySrv.URL},
	}

	// Complete routes through the proxy.
	adapter := NewOpenAIAdapter(cfg)
	resp, err := adapter.Complete(context.Background(), CompleteRequest{
		Model: "m", SystemPrompt: "s", Prompt: "p",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !strings.Contains(resp.Content, "proxy") {
		t.Errorf("complete content = %q, want the proxy response", resp.Content)
	}
	if originHits != 0 {
		t.Errorf("origin was contacted %d times directly", originHits)
	}

	// Stream routes through the proxy too (no overall timeout on this path).
	var got strings.Builder
	err = adapter.Stream(context.Background(), StreamRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(d StreamDelta) { got.WriteString(d.Content) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got.String() != "via-proxy" {
		t.Errorf("stream content = %q", got.String())
	}
	if originHits != 0 {
		t.Errorf("origin was contacted %d times directly", originHits)
	}

	// mode "none" bypasses any environment proxy: the origin is hit directly.
	direct := NewOpenAIAdapter(AdapterConfig{
		BaseURL: proxySrv.URL, // the "origin" here is the proxy server itself
		APIKey:  "k",
		Proxy:   proxy.Settings{Mode: proxy.ModeNone},
		Timeout: 5 * time.Second,
	})
	if _, err := direct.Complete(context.Background(), CompleteRequest{Model: "m", Prompt: "p"}); err != nil {
		t.Fatalf("complete with mode none: %v", err)
	}
}

// TestTestConnectionHonorsProxySettings verifies the connection-test probes
// use the configured proxy as well.
func TestTestConnectionHonorsProxySettings(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer proxySrv.Close()

	originHits := 0
	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer originSrv.Close()

	res := TestConnection(context.Background(), TestConfig{
		Mode:    ModeOpenAICompatible,
		BaseURL: originSrv.URL,
		APIKey:  "k",
		Model:   "m",
		Proxy:   proxy.Settings{Mode: proxy.ModeHTTP, URL: proxySrv.URL},
	})
	if !res.OK {
		t.Fatalf("test connection through proxy failed: %s", res.Message)
	}
	if originHits != 0 {
		t.Errorf("origin was contacted %d times directly", originHits)
	}
}
