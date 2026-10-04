package api

import (
	"database/sql"
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

// chatTestEnv is the full Phase 4 test environment: handler + the mock
// provider behind it + the database (for storage-level assertions).
type chatTestEnv struct {
	h    http.Handler
	mock *translation.MockProvider
	db   *sql.DB
}

// newChatTestEnv builds the API handler with the mock resolver wired into
// BOTH the translation pipeline and the chat service.
func newChatTestEnv(t *testing.T) *chatTestEnv {
	t.Helper()
	return newChatTestEnvWithResolver(t, nil)
}

func newChatTestEnvWithResolver(t *testing.T, resolver llm.Resolver) *chatTestEnv {
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
	vocabularyRepo := repository.NewVocabularyRepo(db)
	chatsRepo := repository.NewChatsRepo(db)
	terms, err := terminology.NewService(t.Context(), repository.NewTerminologyRepo(db))
	if err != nil {
		t.Fatalf("terminology: %v", err)
	}
	provider := translation.NewMockProvider()
	if resolver == nil {
		resolver = llm.FixedResolver{P: provider}
	}
	providerSettings := NewProviderSettingsStore(settingsRepo, credential.NewMemory(), logging.NewRedactor())
	pipeline, err := translation.NewPipeline(resolver, providerSettings.TranslationModel, terms, cacheRepo, historyRepo, settingsRepo)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	chatSvc := chat.NewService(chatsRepo, settingsRepo, resolver, providerSettings.ChatModel, nil)
	logger := logging.New(io.Discard, slog.LevelError, logging.NewRedactor())
	handler := NewServer(cfg, logger, pipeline, settingsRepo, historyRepo, cacheRepo, tabsRepo, vocabularyRepo,
		terms, chatsRepo, chatSvc, providerSettings, resolver, db.Ping).Handler()
	return &chatTestEnv{h: handler, mock: provider, db: db}
}

// chatSummary queries chats.compact_summary directly.
func (env *chatTestEnv) chatSummary(t *testing.T, chatID string) string {
	t.Helper()
	var summary string
	if err := env.db.QueryRow(`SELECT compact_summary FROM chats WHERE id = ?`, chatID).Scan(&summary); err != nil {
		t.Fatalf("query compact summary: %v", err)
	}
	return summary
}

// messageRoles returns the ordered (role, content) pairs of a chat.
func (env *chatTestEnv) messageRows(t *testing.T, chatID string) [][2]string {
	t.Helper()
	rows, err := env.db.Query(`SELECT role, content FROM messages WHERE chat_id = ?
		ORDER BY created_at, id`, chatID)
	if err != nil {
		t.Fatalf("query messages: %v", err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			t.Fatalf("scan messages: %v", err)
		}
		out = append(out, [2]string{role, content})
	}
	return out
}

func TestChatsCRUDAndCascade(t *testing.T) {
	env := newChatTestEnv(t)

	// Create with default title.
	status, body := do(t, env.h, http.MethodPost, "/api/v1/chats", testToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d (%v)", status, body)
	}
	chatID, _ := body["id"].(string)
	if len(chatID) != 26 {
		t.Fatalf("chat id %q is not a ULID", chatID)
	}
	if body["title"] != "新会话" {
		t.Errorf("default title = %v", body["title"])
	}
	if body["created_at"] == "" || body["updated_at"] == "" {
		t.Errorf("timestamps missing: %v", body)
	}

	// Create with explicit title, later in time → must list first.
	time.Sleep(1100 * time.Millisecond) // RFC3339 second precision
	status, body2 := do(t, env.h, http.MethodPost, "/api/v1/chats", testToken,
		map[string]any{"title": "自定义会话"})
	if status != http.StatusCreated || body2["title"] != "自定义会话" {
		t.Fatalf("create custom = %d %v", status, body2)
	}
	chatID2, _ := body2["id"].(string)

	status, list := do(t, env.h, http.MethodGet, "/api/v1/chats", testToken, nil)
	if status != 200 {
		t.Fatalf("list status = %d (%v)", status, list)
	}
	rawList := rawJSONRequest(t, env.h, http.MethodGet, "/api/v1/chats", testToken, nil)
	var chats []map[string]any
	if err := json.Unmarshal(rawList, &chats); err != nil {
		t.Fatalf("chat list is not a bare array: %v (%s)", err, rawList)
	}
	if len(chats) != 2 || chats[0]["id"] != chatID2 || chats[1]["id"] != chatID {
		t.Errorf("chat list order = %+v, want newest activity first", chats)
	}

	// PATCH rename.
	status, upd := do(t, env.h, http.MethodPatch, "/api/v1/chats/"+chatID, testToken,
		map[string]any{"title": "改名了"})
	if status != 200 || upd["title"] != "改名了" {
		t.Errorf("patch = %d %v", status, upd)
	}
	// PATCH validation and 404.
	status, _ = do(t, env.h, http.MethodPatch, "/api/v1/chats/"+chatID, testToken,
		map[string]any{"title": "  "})
	if status != http.StatusBadRequest {
		t.Errorf("patch empty title = %d, want 400", status)
	}
	status, _ = do(t, env.h, http.MethodPatch, "/api/v1/chats/UNKNOWN01", testToken,
		map[string]any{"title": "x"})
	if status != http.StatusNotFound {
		t.Errorf("patch unknown = %d, want 404", status)
	}

	// Cascade: messages of chatID disappear with the chat.
	if s, b := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": "hi", "thinking": false}); s != 200 {
		t.Fatalf("generate = %d (%v)", s, b)
	}
	if rows := env.messageRows(t, chatID); len(rows) != 2 {
		t.Fatalf("messages before delete = %d, want 2", len(rows))
	}
	status, _ = do(t, env.h, http.MethodDelete, "/api/v1/chats/"+chatID, testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete chat = %d, want 204", status)
	}
	if rows := env.messageRows(t, chatID); len(rows) != 0 {
		t.Errorf("messages not cascaded: %v", rows)
	}
	status, _ = do(t, env.h, http.MethodDelete, "/api/v1/chats/"+chatID, testToken, nil)
	if status != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", status)
	}
}

func TestChatsMessagesListingAndClear(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	// Unknown chat → 404 for both message endpoints.
	if s, _ := do(t, env.h, http.MethodGet, "/api/v1/chats/UNKNOWN01/messages", testToken, nil); s != 404 {
		t.Errorf("list messages of unknown chat = %d, want 404", s)
	}
	if s, _ := do(t, env.h, http.MethodDelete, "/api/v1/chats/UNKNOWN01/messages", testToken, nil); s != 404 {
		t.Errorf("clear messages of unknown chat = %d, want 404", s)
	}

	// One exchange.
	if s, b := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": "hello", "thinking": false}); s != 200 {
		t.Fatalf("generate = %d (%v)", s, b)
	}

	// Conversation context survives /clear; compact summary does not.
	if s, _ := do(t, env.h, http.MethodPut, "/api/v1/chats/"+chatID+"/context", testToken,
		map[string]any{"content": "保留的会话上下文"}); s != 204 {
		t.Fatalf("put conversation context = %d", s)
	}
	if s, b := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/compact", testToken, nil); s != 200 {
		t.Fatalf("compact = %d (%v)", s, b)
	}
	if env.chatSummary(t, chatID) == "" {
		t.Fatal("compact summary should be stored before clear")
	}

	status, _ := do(t, env.h, http.MethodDelete, "/api/v1/chats/"+chatID+"/messages", testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("clear = %d, want 204", status)
	}
	rawMsgs := rawJSONRequest(t, env.h, http.MethodGet, "/api/v1/chats/"+chatID+"/messages", testToken, nil)
	var messages []map[string]any
	if err := json.Unmarshal(rawMsgs, &messages); err != nil {
		t.Fatalf("messages is not a bare array: %v (%s)", err, rawMsgs)
	}
	if len(messages) != 0 {
		t.Errorf("messages after clear = %v", messages)
	}
	if env.chatSummary(t, chatID) != "" {
		t.Errorf("compact summary must be cleared")
	}
	status, ctxBody := do(t, env.h, http.MethodGet, "/api/v1/chats/"+chatID+"/context", testToken, nil)
	if status != 200 || ctxBody["content"] != "保留的会话上下文" {
		t.Errorf("conversation context after clear = %d %v, want kept", status, ctxBody)
	}
}

func TestChatsMessagesAscendingWithReasoning(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")
	if s, _ := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": "hello", "thinking": true}); s != 200 {
		t.Fatal("generate failed")
	}
	rawMsgs := rawJSONRequest(t, env.h, http.MethodGet, "/api/v1/chats/"+chatID+"/messages", testToken, nil)
	var messages []struct {
		ID               string `json:"id"`
		Role             string `json:"role"`
		Content          string `json:"content"`
		ReasoningContent string `json:"reasoning_content"`
		CreatedAt        string `json:"created_at"`
	}
	if err := json.Unmarshal(rawMsgs, &messages); err != nil {
		t.Fatalf("messages decode: %v (%s)", err, rawMsgs)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Errorf("user row = %+v", messages[0])
	}
	if messages[1].Role != "assistant" {
		t.Errorf("assistant row role = %s", messages[1].Role)
	}
	// Mock emitted reasoning (thinking=true request) — persisted on the row.
	if messages[1].ReasoningContent != "（mock 思考过程）" {
		t.Errorf("assistant reasoning_content = %q", messages[1].ReasoningContent)
	}
}

func TestChatCompactEndpoint(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")
	if s, _ := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": "hello", "thinking": false}); s != 200 {
		t.Fatal("generate failed")
	}
	before := env.mock.ChatCompleteCalls()
	status, body := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/compact", testToken, nil)
	if status != 200 {
		t.Fatalf("compact = %d (%v)", status, body)
	}
	summary, _ := body["summary"].(string)
	if summary == "" || !strings.Contains(summary, "mock 摘要") {
		t.Errorf("summary = %q", summary)
	}
	if env.mock.ChatCompleteCalls() != before+1 {
		t.Errorf("ChatComplete calls = %d, want %d", env.mock.ChatCompleteCalls(), before+1)
	}
	if env.chatSummary(t, chatID) != summary {
		t.Errorf("stored summary mismatch: %q vs %q", env.chatSummary(t, chatID), summary)
	}

	// Unknown chat → 404; not configured → PROVIDER_NOT_CONFIGURED.
	if s, b := do(t, env.h, http.MethodPost, "/api/v1/chats/UNKNOWN01/compact", testToken, nil); s != 404 {
		t.Errorf("compact unknown = %d (%v), want 404", s, b)
	}
	env2 := newChatTestEnvWithResolver(t, llm.NotConfiguredResolver{})
	id2 := createChat(t, env2.h, "")
	if s, b := do(t, env2.h, http.MethodPost, "/api/v1/chats/"+id2+"/compact", testToken, nil); s != http.StatusBadRequest {
		t.Errorf("compact not configured = %d (%v), want 400", s, b)
	} else if code, _ := errCode(t, b); code != "PROVIDER_NOT_CONFIGURED" {
		t.Errorf("compact not configured code = %v", b)
	}
}

func TestContextEndpointsGlobalAndConversation(t *testing.T) {
	env := newChatTestEnv(t)

	// Global context starts empty.
	status, body := do(t, env.h, http.MethodGet, "/api/v1/context/global", testToken, nil)
	if status != 200 || body["content"] != "" {
		t.Fatalf("initial global context = %d %v", status, body)
	}
	if s, _ := do(t, env.h, http.MethodPut, "/api/v1/context/global", testToken,
		map[string]any{"content": "永远用中文回答"}); s != 204 {
		t.Fatalf("put global context = %d", s)
	}
	status, body = do(t, env.h, http.MethodGet, "/api/v1/context/global", testToken, nil)
	if status != 200 || body["content"] != "永远用中文回答" {
		t.Errorf("global context after put = %d %v", status, body)
	}
	// Stored in the settings kv blob as global_context.
	_, settings := do(t, env.h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if settings["global_context"] != "永远用中文回答" {
		t.Errorf("settings global_context = %v", settings["global_context"])
	}
	// PUT replaces the whole value; an empty content clears the context.
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/context/global", testToken,
		map[string]any{"content": ""})
	if status != 204 {
		t.Errorf("put empty global context = %d, want 204", status)
	}
	status, body = do(t, env.h, http.MethodGet, "/api/v1/context/global", testToken, nil)
	if status != 200 || body["content"] != "" {
		t.Errorf("global context after clearing put = %d %v", status, body)
	}

	// Conversation context round-trip + 404s.
	chatID := createChat(t, env.h, "")
	status, body = do(t, env.h, http.MethodGet, "/api/v1/chats/"+chatID+"/context", testToken, nil)
	if status != 200 || body["content"] != "" {
		t.Fatalf("initial conversation context = %d %v", status, body)
	}
	if s, _ := do(t, env.h, http.MethodPut, "/api/v1/chats/"+chatID+"/context", testToken,
		map[string]any{"content": "项目背景：Go 后端"}); s != 204 {
		t.Fatalf("put conversation context = %d", s)
	}
	status, body = do(t, env.h, http.MethodGet, "/api/v1/chats/"+chatID+"/context", testToken, nil)
	if status != 200 || body["content"] != "项目背景：Go 后端" {
		t.Errorf("conversation context after put = %d %v", status, body)
	}
	if s, _ := do(t, env.h, http.MethodGet, "/api/v1/chats/UNKNOWN01/context", testToken, nil); s != 404 {
		t.Errorf("unknown chat context get = %d, want 404", s)
	}
	if s, _ := do(t, env.h, http.MethodPut, "/api/v1/chats/UNKNOWN01/context", testToken,
		map[string]any{"content": "x"}); s != 404 {
		t.Errorf("unknown chat context put = %d, want 404", s)
	}
}

func TestAutoCompactTriggeredOverHTTP(t *testing.T) {
	env := newChatTestEnv(t)
	// effective context = 40 tokens; threshold 0.8 → compact above 32 tokens.
	if s, _ := do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"max_context_tokens": 40, "auto_compact": true, "compact_threshold": 0.8}); s != 200 {
		t.Fatal("put settings failed")
	}
	chatID := createChat(t, env.h, "")
	long := strings.Repeat("x", 400) // ≈100 tokens with the assistant echo ≫ 32
	status, body := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": long, "thinking": false})
	if status != 200 {
		t.Fatalf("generate = %d (%v)", status, body)
	}
	if calls := env.mock.ChatCompleteCalls(); calls != 1 {
		t.Errorf("ChatComplete calls = %d, want 1 (auto compact at threshold)", calls)
	}
	if env.chatSummary(t, chatID) == "" {
		t.Errorf("auto compact summary not stored")
	}

	// A further short generation still completes. Because 1.0 keeps the raw
	// history in the estimate (docs/06 §10: rows are never deleted), the
	// accumulated transcript stays above the tiny threshold, so auto compact
	// runs again — the invariant is that the generation itself never breaks.
	if s, b := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": "hi", "thinking": false}); s != 200 {
		t.Fatalf("second generate = %d (%v)", s, b)
	}
	if calls := env.mock.ChatCompleteCalls(); calls != 2 {
		t.Errorf("ChatComplete calls after short chat = %d, want 2", calls)
	}
}

func TestChatsAuthOnAllEndpoints(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/chats"},
		{http.MethodPost, "/api/v1/chats"},
		{http.MethodPatch, "/api/v1/chats/" + chatID},
		{http.MethodDelete, "/api/v1/chats/" + chatID},
		{http.MethodGet, "/api/v1/chats/" + chatID + "/messages"},
		{http.MethodDelete, "/api/v1/chats/" + chatID + "/messages"},
		{http.MethodGet, "/api/v1/chats/" + chatID + "/context"},
		{http.MethodPut, "/api/v1/chats/" + chatID + "/context"},
		{http.MethodPost, "/api/v1/chats/" + chatID + "/compact"},
		{http.MethodPost, "/api/v1/chats/" + chatID + "/generations"},
		{http.MethodPost, "/api/v1/chats/" + chatID + "/regenerate"},
		{http.MethodPost, "/api/v1/chats/" + chatID + "/generations/GEN01/cancel"},
		{http.MethodGet, "/api/v1/context/global"},
		{http.MethodPut, "/api/v1/context/global"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token = %d, want 401", c.method, c.path, rec.Code)
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Errorf("%s %s 401 body is not the error envelope: %v", c.method, c.path, err)
			continue
		}
		if code, _ := errCode(t, parsed); code != "UNAUTHORIZED_LOCAL_SESSION" {
			t.Errorf("%s %s 401 code = %v", c.method, c.path, code)
		}
	}
}

// --- shared helpers ---

// createChat creates a chat through the API and returns its id.
func createChat(t *testing.T, h http.Handler, title string) string {
	t.Helper()
	var body any
	if title != "" {
		body = map[string]any{"title": title}
	}
	status, parsed := do(t, h, http.MethodPost, "/api/v1/chats", testToken, body)
	if status != http.StatusCreated {
		t.Fatalf("create chat = %d (%v)", status, parsed)
	}
	id, _ := parsed["id"].(string)
	if id == "" {
		t.Fatalf("created chat has no id: %v", parsed)
	}
	return id
}

// rawJSONRequest performs a request and returns the raw body bytes.
func rawJSONRequest(t *testing.T, h http.Handler, method, path, token string, body any) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s %s = %d (%s)", method, path, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}
