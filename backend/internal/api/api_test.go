package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Weber-5/FloatTranslate/backend/internal/config"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/logging"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/terminology"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

const testToken = "testtoken"

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{HTTPPort: "0", SessionToken: testToken, Version: config.DefaultVersion}
	db, err := database.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	settingsRepo := repository.NewSettingsRepo(db)
	historyRepo := repository.NewHistoryRepo(db)
	cacheRepo := repository.NewCacheRepo(db)
	tabsRepo := repository.NewTabsRepo(db)
	terms, err := terminology.NewService(t.Context(), repository.NewTerminologyRepo(db))
	if err != nil {
		t.Fatalf("terminology: %v", err)
	}
	provider := translation.NewMockProvider()
	providerSettings := NewProviderSettingsStore(settingsRepo)
	pipeline, err := translation.NewPipeline(provider, providerSettings.TranslationModel, terms, cacheRepo, historyRepo)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	logger := logging.New(io.Discard, slog.LevelError, logging.NewRedactor())
	return NewServer(cfg, logger, pipeline, settingsRepo, historyRepo, tabsRepo, terms, provider, db.Ping).Handler()
}

// do performs a request and returns status + decoded JSON body (nil when the
// body is not JSON, e.g. 204).
func do(t *testing.T, h http.Handler, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var parsed map[string]any
	if rec.Body != nil {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec.Code, parsed
}

func errCode(t *testing.T, body map[string]any) (string, bool) {
	t.Helper()
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("body has no error object: %v", body)
	}
	code, _ := errObj["code"].(string)
	retryable, _ := errObj["retryable"].(bool)
	return code, retryable
}

func TestHealthNoAuth(t *testing.T) {
	h := newTestHandler(t)
	status, body := do(t, h, http.MethodGet, "/health", "", nil)
	if status != http.StatusOK {
		t.Fatalf("health status = %d, want 200", status)
	}
	if body["status"] != "ready" || body["version"] != "1.0.0" || body["db_status"] != "ok" {
		t.Errorf("health body = %v", body)
	}
}

func TestAuthMiddleware(t *testing.T) {
	h := newTestHandler(t)

	// No token.
	status, body := do(t, h, http.MethodGet, "/api/v1/settings", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("no token status = %d, want 401", status)
	}
	if code, retryable := errCode(t, body); code != "UNAUTHORIZED_LOCAL_SESSION" || retryable {
		t.Errorf("no token envelope = %v", body)
	}

	// Wrong token.
	status, body = do(t, h, http.MethodGet, "/api/v1/settings", "wrongtoken", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", status)
	}
	if code, _ := errCode(t, body); code != "UNAUTHORIZED_LOCAL_SESSION" {
		t.Errorf("wrong token envelope = %v", body)
	}

	// Unauthorized translations endpoint.
	status, body = do(t, h, http.MethodPost, "/api/v1/translations", "", map[string]any{"text": "hello"})
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthorized translation status = %d, want 401", status)
	}
	if code, _ := errCode(t, body); code != "UNAUTHORIZED_LOCAL_SESSION" {
		t.Errorf("translation envelope = %v", body)
	}

	// Correct token passes.
	status, _ = do(t, h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", status)
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translations", strings.NewReader("not json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var parsed struct {
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Retryable bool           `json:"retryable"`
			Details   map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("envelope is not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if parsed.Error.Code != "INVALID_REQUEST" || parsed.Error.Message == "" {
		t.Errorf("envelope shape wrong: %+v", parsed.Error)
	}
}

func TestCreateTranslationSmoke(t *testing.T) {
	h := newTestHandler(t)
	status, body := do(t, h, http.MethodPost, "/api/v1/translations", testToken,
		map[string]any{"text": "suspended"})
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%v", status, body)
	}
	if body["kind"] != "word" || body["source"] != "model" || body["model"] != "deepseek-flash" {
		t.Errorf("response fields wrong: %v", body)
	}
	id, _ := body["translation_id"].(string)
	if len(id) != 26 {
		t.Errorf("translation_id %q is not a 26-char ULID", id)
	}
	result, _ := body["result"].(map[string]any)
	if result["word"] != "suspended" {
		t.Errorf("result.word = %v", result["word"])
	}
	if _, ok := body["created_at"].(string); !ok {
		t.Errorf("created_at missing")
	}
}

func TestHistoryEndpoints(t *testing.T) {
	h := newTestHandler(t)
	if s, b := do(t, h, http.MethodPost, "/api/v1/translations", testToken, map[string]any{"text": "suspended"}); s != 200 {
		t.Fatalf("translate 1: %d %v", s, b)
	}
	if s, b := do(t, h, http.MethodPost, "/api/v1/translations", testToken, map[string]any{"text": "Hello, world!"}); s != 200 {
		t.Fatalf("translate 2: %d %v", s, b)
	}

	status, body := do(t, h, http.MethodGet, "/api/v1/history", testToken, nil)
	if status != 200 {
		t.Fatalf("history status = %d", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("history items = %d, want 2", len(items))
	}
	first, _ := items[0].(map[string]any)
	id, _ := first["id"].(string)

	status, _ = do(t, h, http.MethodDelete, "/api/v1/history/"+id, testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete item status = %d", status)
	}
	_, body = do(t, h, http.MethodGet, "/api/v1/history", testToken, nil)
	items, _ = body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("after delete items = %d, want 1", len(items))
	}

	status, _ = do(t, h, http.MethodDelete, "/api/v1/history", testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("clear status = %d", status)
	}
	_, body = do(t, h, http.MethodGet, "/api/v1/history", testToken, nil)
	items, _ = body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("after clear items = %d, want 0", len(items))
	}
}

func TestTabsRoundTrip(t *testing.T) {
	h := newTestHandler(t)
	tabs := []map[string]any{
		{"id": "t1", "kind": "word", "title": "hello", "position": 1, "is_active": false, "payload": map[string]any{"x": 1}},
		{"id": "t2", "kind": "text", "title": "doc", "position": 0, "is_active": true, "payload": map[string]any{}},
	}
	raw, _ := json.Marshal(tabs)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tabs", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("put tabs status = %d (%s)", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/tabs", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("tabs response is not a bare array: %v (%s)", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("tabs = %d, want 2", len(got))
	}
	if got[0]["id"] != "t2" || got[1]["id"] != "t1" {
		t.Errorf("tabs not ordered by position: %v", got)
	}
	if got[0]["is_active"] != true {
		t.Errorf("t2 should be active")
	}
}

func TestTerminologyEndpoints(t *testing.T) {
	h := newTestHandler(t)
	status, body := do(t, h, http.MethodPost, "/api/v1/terminology", testToken,
		map[string]any{"source": "API", "target": "接口"})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d (%v)", status, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("created terminology missing id: %v", body)
	}

	status, _ = do(t, h, http.MethodPost, "/api/v1/terminology", testToken,
		map[string]any{"source": "api", "target": "重复"})
	if status != http.StatusConflict {
		t.Errorf("case-insensitive duplicate should be 409, got %d", status)
	}

	status, _ = do(t, h, http.MethodPut, "/api/v1/terminology/"+id, testToken,
		map[string]any{"source": "API", "target": "应用程序接口"})
	if status != http.StatusOK {
		t.Errorf("update status = %d", status)
	}

	status, _ = do(t, h, http.MethodDelete, "/api/v1/terminology/"+id, testToken, nil)
	if status != http.StatusNoContent {
		t.Errorf("delete status = %d", status)
	}
}

func TestTerminologyEndpointsBareArray(t *testing.T) {
	h := newTestHandler(t)
	if s, _ := do(t, h, http.MethodPost, "/api/v1/terminology", testToken,
		map[string]any{"source": "API", "target": "接口"}); s != http.StatusCreated {
		t.Fatal("create failed")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/terminology", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("terminology response is not a bare array: %v (%s)", err, rec.Body.String())
	}
	if len(got) != 1 || got[0]["target"] != "接口" {
		t.Errorf("terminology list wrong: %v", got)
	}
}

func TestProviderSettingsAndKeyMasking(t *testing.T) {
	h := newTestHandler(t)
	status, body := do(t, h, http.MethodGet, "/api/v1/settings/provider", testToken, nil)
	if status != 200 {
		t.Fatalf("get provider status = %d", status)
	}
	if body["mode"] != "deepseek" || body["base_url"] != "https://api.deepseek.com" ||
		body["translation_model"] != "deepseek-flash" || body["chat_model"] != "deepseek-flash" {
		t.Errorf("provider defaults wrong: %v", body)
	}
	if body["api_key_configured"] != false {
		t.Errorf("api_key_configured should start false: %v", body)
	}

	status, body = do(t, h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "openai_compatible", "base_url": "https://example.com/v1",
		"translation_model": "m1", "chat_model": "m2", "api_key": "sk-secret-123",
	})
	if status != 200 {
		t.Fatalf("put provider status = %d (%v)", status, body)
	}
	if body["api_key_configured"] != true {
		t.Errorf("api_key_configured should be true: %v", body)
	}
	if hint, _ := body["api_key_hint"].(string); hint != "sk-..." {
		t.Errorf("api_key_hint = %q, want sk-...", hint)
	}
	if strings.Contains(bodyString(t, body), "sk-secret-123") {
		t.Errorf("provider settings view must never return the raw key")
	}

	// GET /settings kv blob must not contain the key either.
	status, kv := do(t, h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if status != 200 {
		t.Fatalf("get settings status = %d", status)
	}
	if strings.Contains(bodyString(t, kv), "sk-secret-123") {
		t.Errorf("settings kv blob leaked api key")
	}

	// Test connection endpoint.
	status, body = do(t, h, http.MethodPost, "/api/v1/settings/provider/test", testToken, nil)
	if status != 200 || body["ok"] != true || body["message"] != "测试连接成功（mock）" {
		t.Errorf("provider test = %d %v", status, body)
	}
}

func TestRuntimeCapabilities(t *testing.T) {
	h := newTestHandler(t)
	status, body := do(t, h, http.MethodGet, "/api/v1/runtime/capabilities", testToken, nil)
	if status != 200 {
		t.Fatalf("capabilities status = %d", status)
	}
	want := map[string]any{
		"configured_context_tokens": float64(1000000), "effective_context_tokens": float64(1000000),
		"configured_output_tokens": float64(8192), "effective_output_tokens": float64(8192),
		"supports_thinking": true, "supports_structured_output": true,
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("capabilities[%s] = %v, want %v", k, body[k], v)
		}
	}
}

func bodyString(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}
