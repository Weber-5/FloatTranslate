package api

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/chat"
	"github.com/Weber-5/FloatTranslate/backend/internal/config"
	"github.com/Weber-5/FloatTranslate/backend/internal/credential"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/logging"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/terminology"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

const testToken = "testtoken"

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return newTestHandlerWithResolver(t, nil)
}

// newTestHandlerWithCredential builds the handler on the given database and
// credential store (used by the backup/clear/reset tests, which need to seed
// rows and inspect credential state).
func newTestHandlerWithCredential(t *testing.T, db *sql.DB, cred credential.Store) http.Handler {
	t.Helper()
	return newTestHandlerFull(t, db, cred, nil)
}

// newTestHandlerWithResolver builds the API handler; when resolver is nil a
// mock-configured resolver is used (the mock is a test fixture injected via
// llm.Resolver, never reachable through the production wiring).
func newTestHandlerWithResolver(t *testing.T, resolver llm.Resolver) http.Handler {
	t.Helper()
	return newTestHandlerFull(t, nil, nil, resolver)
}

func newTestHandlerFull(t *testing.T, existingDB *sql.DB, cred credential.Store, resolver llm.Resolver) http.Handler {
	t.Helper()
	cfg := config.Config{HTTPPort: "0", SessionToken: testToken, Version: config.DefaultVersion}
	db := existingDB
	if db == nil {
		var err error
		db, err = database.Open(filepath.Join(t.TempDir(), "api.db"))
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
	}
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	settingsRepo := repository.NewSettingsRepo(db)
	historyRepo := repository.NewHistoryRepo(db)
	cacheRepo := repository.NewCacheRepo(db)
	tabsRepo := repository.NewTabsRepo(db)
	vocabularyRepo := repository.NewVocabularyRepo(db)
	terms, err := terminology.NewService(t.Context(), repository.NewTerminologyRepo(db))
	if err != nil {
		t.Fatalf("terminology: %v", err)
	}
	provider := translation.NewMockProvider()
	if resolver == nil {
		resolver = llm.FixedResolver{P: provider}
	}
	if cred == nil {
		cred = credential.NewMemory()
	}
	providerSettings := NewProviderSettingsStore(settingsRepo, cred, logging.NewRedactor())
	pipeline, err := translation.NewPipeline(resolver, providerSettings.TranslationModel, terms, cacheRepo, historyRepo, settingsRepo)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	chatsRepo := repository.NewChatsRepo(db)
	chatSvc := chat.NewService(chatsRepo, settingsRepo, resolver, providerSettings.ChatModel, nil)
	logger := logging.New(io.Discard, slog.LevelError, logging.NewRedactor())
	return NewServer(cfg, logger, pipeline, settingsRepo, historyRepo, cacheRepo, tabsRepo, vocabularyRepo,
		terms, chatsRepo, chatSvc, providerSettings, resolver, db.Ping).Handler()
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
	if hint, _ := body["api_key_hint"].(string); hint != "sk-…" {
		t.Errorf("api_key_hint = %q, want sk-… (first 3 chars + ellipsis)", hint)
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

	// Test connection without a saved key fails fast without any request.
	h2 := newTestHandler(t)
	status, body = do(t, h2, http.MethodPost, "/api/v1/settings/provider/test", testToken, nil)
	if status != 200 || body["ok"] != false {
		t.Fatalf("provider test without key = %d %v", status, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "API Key") {
		t.Errorf("test without key message = %q", msg)
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

// --- Phase 2 API tests ---

func TestProviderNotConfiguredOverHTTP(t *testing.T) {
	h := newTestHandlerWithResolver(t, llm.NotConfiguredResolver{})
	status, body := do(t, h, http.MethodPost, "/api/v1/translations", testToken,
		map[string]any{"text": "suspended"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%v)", status, body)
	}
	code, retryable := errCode(t, body)
	if code != "PROVIDER_NOT_CONFIGURED" || retryable {
		t.Errorf("envelope = %v", body)
	}
}

func TestLanguageGateOverHTTP(t *testing.T) {
	h := newTestHandler(t)
	status, body := do(t, h, http.MethodPost, "/api/v1/translations", testToken,
		map[string]any{"text": "这是一段纯中文的输入内容"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	code, retryable := errCode(t, body)
	if code != "UNSUPPORTED_LANGUAGE" || retryable {
		t.Fatalf("envelope = %v", body)
	}
	errObj, _ := body["error"].(map[string]any)
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "暂仅支持英译中") {
		t.Errorf("message = %q", msg)
	}

	// English input is fine.
	status, _ = do(t, h, http.MethodPost, "/api/v1/translations", testToken,
		map[string]any{"text": "capability"})
	if status != 200 {
		t.Errorf("english input status = %d, want 200", status)
	}
}

func wordBody(word string) map[string]any {
	return map[string]any{
		"word": word, "lemma": word, "phonetic_uk": "/" + word + "/", "phonetic_us": "/" + word + "/",
		"parts_of_speech": []any{map[string]any{"part": "verb", "meanings": []any{"跑", "奔跑"}}},
		"synonyms":        []any{"jog"}, "inflections": []any{word + "s"},
	}
}

func TestVocabularyEndpoints(t *testing.T) {
	h := newTestHandler(t)

	// PUT (save) — 200 with the stored item.
	status, body := do(t, h, http.MethodPut, "/api/v1/vocabulary/run", testToken, wordBody("run"))
	if status != 200 {
		t.Fatalf("save status = %d (%v)", status, body)
	}
	if body["lemma"] != "run" || body["saved_at"] == "" || body["last_viewed_at"] == "" {
		t.Errorf("saved item = %v", body)
	}
	firstSavedAt, _ := body["saved_at"].(string)

	// Idempotent upsert: same lemma different case updates word_json and
	// last_viewed_at, no duplicate, saved_at kept.
	updated := wordBody("RUN")
	updated["parts_of_speech"] = []any{map[string]any{"part": "noun", "meanings": []any{"奔跑者"}}}
	time.Sleep(1100 * time.Millisecond) // RFC3339 second precision
	status, body = do(t, h, http.MethodPut, "/api/v1/vocabulary/RUN", testToken, updated)
	if status != 200 {
		t.Fatalf("re-save status = %d (%v)", status, body)
	}
	if body["saved_at"] != firstSavedAt {
		t.Errorf("saved_at changed on re-save: %v", body["saved_at"])
	}
	if body["last_viewed_at"] == firstSavedAt {
		t.Errorf("last_viewed_at not refreshed: %v", body["last_viewed_at"])
	}

	// GET list: exactly one item, word_json updated.
	status, _ = do(t, h, http.MethodGet, "/api/v1/vocabulary", testToken, nil)
	if status != 200 {
		t.Fatalf("list status = %d", status)
	}
	raw := httptest.NewRequest(http.MethodGet, "/api/v1/vocabulary", nil)
	raw.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, raw)
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("vocabulary response is not a bare array: %v (%s)", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("vocabulary items = %d, want 1 (NOCASE dedupe)", len(got))
	}
	if got[0]["lemma"] != "run" {
		t.Errorf("lemma = %v, want original spelling run", got[0]["lemma"])
	}
	word, _ := got[0]["word"].(map[string]any)
	if word["word"] != "RUN" || word["lemma"] != "RUN" {
		t.Errorf("word_json not updated on re-save: %v", word)
	}

	// Query filter on meaning.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vocabulary?query=%E5%A5%94%E8%B7%91%E8%80%85", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("query response invalid: %v", err)
	}
	if len(got) != 1 || got[0]["lemma"] != "run" {
		t.Errorf("query by meaning = %v", got)
	}

	// URL-decoded lemma path value.
	status, body = do(t, h, http.MethodPut, "/api/v1/vocabulary/Caf%C3%A9", testToken, wordBody("Café"))
	if status != 200 || body["lemma"] != "Café" {
		t.Errorf("url-decoded lemma = %d %v", status, body)
	}

	// Validation: empty word rejected.
	status, _ = do(t, h, http.MethodPut, "/api/v1/vocabulary/empty", testToken, map[string]any{"lemma": "empty"})
	if status != http.StatusBadRequest {
		t.Errorf("empty word status = %d, want 400", status)
	}

	// DELETE → 204, idempotent on unknown lemma.
	for i := 0; i < 2; i++ {
		status, _ = do(t, h, http.MethodDelete, "/api/v1/vocabulary/run", testToken, nil)
		if status != http.StatusNoContent {
			t.Errorf("delete #%d status = %d, want 204", i+1, status)
		}
	}
}

func TestHistoryCursorPaginationHTTP(t *testing.T) {
	h := newTestHandler(t)
	texts := []string{"suspended", "Hello, world!", "capability", "graceful shutdown", "portable format"}
	for _, text := range texts {
		if s, b := do(t, h, http.MethodPost, "/api/v1/translations", testToken, map[string]any{"text": text}); s != 200 {
			t.Fatalf("translate %q: %d %v", text, s, b)
		}
	}

	seen := map[string]bool{}
	var lastOrder []string
	cursor := ""
	for page := 0; page < 4; page++ {
		path := "/api/v1/history?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("page %d status = %d (%s)", page, rec.Code, rec.Body.String())
		}
		var pageBody struct {
			Items []struct {
				ID        string `json:"id"`
				CreatedAt string `json:"created_at"`
				Source    string `json:"source"`
				Model     string `json:"model"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &pageBody); err != nil {
			t.Fatalf("page %d body: %v", page, err)
		}
		for _, item := range pageBody.Items {
			if seen[item.ID] {
				t.Errorf("item %s seen twice", item.ID)
			}
			seen[item.ID] = true
			lastOrder = append(lastOrder, item.CreatedAt+"|"+item.ID)
			// Phase 2 openapi: HistoryItem now carries optional source+model.
			if item.Source == "" || item.Model == "" {
				t.Errorf("history item missing source/model: %+v", item)
			}
		}
		if len(pageBody.Items) > 1 {
			// Stable newest-first ordering within the page.
			if pageBody.Items[0].CreatedAt < pageBody.Items[1].CreatedAt {
				t.Errorf("page not ordered newest-first: %+v", pageBody.Items)
			}
		}
		if pageBody.NextCursor == nil {
			break
		}
		// Cursor is opaque base64url of `<created_at RFC3339>|<id>` of the
		// LAST item on the page.
		raw, err := base64.RawURLEncoding.DecodeString(*pageBody.NextCursor)
		if err != nil {
			t.Fatalf("next_cursor not base64url: %v", err)
		}
		want := pageBody.Items[len(pageBody.Items)-1].CreatedAt + "|" + pageBody.Items[len(pageBody.Items)-1].ID
		if string(raw) != want {
			t.Errorf("cursor payload = %q, want %q", raw, want)
		}
		cursor = *pageBody.NextCursor
	}
	if len(seen) != len(texts) {
		t.Errorf("paged through %d items, want %d", len(seen), len(texts))
	}
	for i := 1; i < len(lastOrder); i++ {
		if lastOrder[i-1] < lastOrder[i] {
			t.Errorf("global order violated: %v", lastOrder)
		}
	}

	// Invalid cursor → 400 INVALID_REQUEST.
	status, body := do(t, h, http.MethodGet, "/api/v1/history?cursor=garbage!", testToken, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", status)
	}
	if code, _ := errCode(t, body); code != "INVALID_REQUEST" {
		t.Errorf("invalid cursor envelope = %v", body)
	}
}

func TestSettingsDefaultsMergeAndPersistence(t *testing.T) {
	h := newTestHandler(t)

	// Defaults for missing keys.
	status, body := do(t, h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if status != 200 {
		t.Fatalf("get settings status = %d", status)
	}
	wantDefaults := map[string]any{
		"theme": "system", "max_context_tokens": float64(1000000), "max_output_tokens": float64(8192),
		"auto_compact": true, "compact_threshold": 0.8, "global_context": "",
		"ai_system_prompt": "", "custom_translation_prompt": "", "always_on_top": true,
		"auto_start": false, "hotkey_show_hide": "Ctrl+Alt+Space",
		"hotkey_translate_selection": "Ctrl+Alt+Q", "proxy_mode": "system", "proxy_url": "",
	}
	for key, want := range wantDefaults {
		if body[key] != want {
			t.Errorf("default %s = %v, want %v", key, body[key], want)
		}
	}

	// PUT persists per-key and returns the merged view.
	status, body = do(t, h, http.MethodPut, "/api/v1/settings", testToken, map[string]any{
		"theme": "dark", "max_context_tokens": 500000,
		"custom_key": map[string]any{"nested": true},
	})
	if status != 200 {
		t.Fatalf("put settings status = %d (%v)", status, body)
	}
	if body["theme"] != "dark" || body["max_context_tokens"] != float64(500000) {
		t.Errorf("updated keys = %v %v", body["theme"], body["max_context_tokens"])
	}
	if body["auto_compact"] != true || body["proxy_mode"] != "system" {
		t.Errorf("defaults must survive the merge: %v", body)
	}
	if body["custom_key"] == nil {
		t.Errorf("arbitrary keys must be persisted")
	}

	// Persistence round-trip.
	_, body = do(t, h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if body["theme"] != "dark" || body["max_context_tokens"] != float64(500000) || body["custom_key"] == nil {
		t.Errorf("settings not persisted: %v", body)
	}

	// The provider row never leaks into the settings blob.
	status, body = do(t, h, http.MethodPut, "/api/v1/settings", testToken, map[string]any{
		"provider": "should be ignored",
	})
	if status != 200 {
		t.Fatalf("put provider key status = %d", status)
	}
	if _, ok := body["provider"]; ok {
		t.Errorf("provider key must not be stored via /settings: %v", body)
	}
}

func TestProviderTestConnectionLive(t *testing.T) {
	// A healthy provider backend: /models returns 200.
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer okSrv.Close()

	// A backend where /models 404s but chat/completions works.
	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer fallbackSrv.Close()

	// An unauthorized backend.
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer badSrv.Close()

	h := newTestHandler(t)

	// Save a key + a working base URL via the override body.
	status, body := do(t, h, http.MethodPost, "/api/v1/settings/provider/test", testToken, map[string]any{
		"mode": "openai_compatible", "base_url": okSrv.URL,
		"translation_model": "m1", "chat_model": "m1", "api_key": "sk-live-999",
	})
	if status != 200 || body["ok"] != true {
		t.Fatalf("test via body override = %d %v", status, body)
	}

	// Fallback probe: /models 404 → chat/completions ping succeeds.
	// (The key must come from the body again — the test endpoint never
	// persists it.)
	status, body = do(t, h, http.MethodPost, "/api/v1/settings/provider/test", testToken, map[string]any{
		"mode": "openai_compatible", "base_url": fallbackSrv.URL,
		"translation_model": "m1", "chat_model": "m1", "api_key": "sk-live-999",
	})
	if status != 200 || body["ok"] != true {
		t.Fatalf("fallback probe = %d %v", status, body)
	}

	// 401 → concise Chinese message, never the key.
	status, body = do(t, h, http.MethodPost, "/api/v1/settings/provider/test", testToken, map[string]any{
		"mode": "openai_compatible", "base_url": badSrv.URL,
		"translation_model": "m1", "chat_model": "m1", "api_key": "sk-live-999",
	})
	if status != 200 || body["ok"] != false {
		t.Fatalf("401 test = %d %v", status, body)
	}
	if msg, _ := body["message"].(string); msg != "API Key 无效或未授权" {
		t.Errorf("401 message = %q", msg)
	}
	if strings.Contains(bodyString(t, body), "sk-live-999") {
		t.Errorf("test response leaked the key")
	}

	// Saved config is used when the body omits provider fields: configure
	// the store (with in-memory key) against okSrv, then test with an
	// empty body.
	status, _ = do(t, h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "openai_compatible", "base_url": okSrv.URL,
		"translation_model": "m1", "chat_model": "m1", "api_key": "sk-live-999",
	})
	if status != 200 {
		t.Fatalf("put provider = %d", status)
	}
	status, body = do(t, h, http.MethodPost, "/api/v1/settings/provider/test", testToken, nil)
	if status != 200 || body["ok"] != true {
		t.Errorf("test with saved config = %d %v", status, body)
	}
}
