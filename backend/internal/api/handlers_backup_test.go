package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/backup"
	"github.com/Weber-5/FloatTranslate/backend/internal/credential"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/logging"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// backupEnv is a test environment with the database and the shared
// in-memory credential store exposed for seeding and assertions.
type backupEnv struct {
	h    http.Handler
	db   *sql.DB
	cred *credential.Memory
}

func newBackupEnv(t *testing.T) *backupEnv {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "backup.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cred := credential.NewMemory()
	h := newTestHandlerWithCredential(t, db, cred)
	return &backupEnv{h: h, db: db, cred: cred}
}

// backupExport is the parsed backup file.
type backupExport struct {
	BackupSchemaVersion int              `json:"backup_schema_version"`
	ExportedAt          string           `json:"exported_at"`
	AppVersion          string           `json:"app_version"`
	Settings            map[string]any   `json:"settings"`
	TranslationHistory  []map[string]any `json:"translation_history"`
	Vocabulary          []map[string]any `json:"vocabulary"`
	Terminology         []map[string]any `json:"terminology"`
	Chats               []map[string]any `json:"chats"`
	Messages            []map[string]any `json:"messages"`
	OpenTabs            []map[string]any `json:"open_tabs"`
	raw                 map[string]json.RawMessage
}

// seed seeds one entry of every business table directly (terminology goes
// through the HTTP handler so the service snapshot is refreshed).
func (env *backupEnv) seed(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Format(time.RFC3339)

	if err := repository.NewHistoryRepo(env.db).Insert(ctx, repository.HistoryRow{
		ID: "hist-1", Kind: "word", InputText: "hello world", NormalizedText: "hello world",
		ResultJSON: `{"word":"hello"}`, Source: "model", Model: "deepseek-flash",
		CreatedAt: now, LastViewedAt: now,
	}); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := repository.NewVocabularyRepo(env.db).Upsert(ctx, repository.VocabularyRow{
		Lemma: "serenity", WordJSON: `{"word":"serenity","lemma":"serenity","phonetic_uk":"","phonetic_us":"","parts_of_speech":[],"synonyms":[],"inflections":[]}`,
		SavedAt: now, LastViewedAt: now,
	}); err != nil {
		t.Fatalf("seed vocabulary: %v", err)
	}
	if s, b := do(t, env.h, http.MethodPost, "/api/v1/terminology", testToken,
		map[string]any{"source": "LLM", "target": "大语言模型"}); s != http.StatusCreated {
		t.Fatalf("seed terminology: %d %v", s, b)
	}
	chats := repository.NewChatsRepo(env.db)
	if err := chats.InsertChat(ctx, repository.ChatRow{
		ID: "chat-1", Title: "Session", ConversationContext: "ctx", CompactSummary: "",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed chat: %v", err)
	}
	gid := "gen-1"
	if err := chats.InsertMessage(ctx, repository.MessageRow{
		ID: "msg-1", ChatID: "chat-1", Role: "user", Content: "hi",
		ReasoningContent: "", CreatedAt: now, GenerationID: sql.NullString{String: gid, Valid: true},
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := repository.NewTabsRepo(env.db).ReplaceAll(ctx, []repository.TabRow{
		{ID: "tab-1", Kind: "word", Title: "hello", TranslationID: sql.NullString{String: "hist-1", Valid: true},
			PayloadJSON: `{"lemma":"hello"}`, Position: 0, IsActive: true, UpdatedAt: now},
	}); err != nil {
		t.Fatalf("seed tabs: %v", err)
	}
}

// export runs POST /backup/export and returns the parsed document.
func (env *backupEnv) export(t *testing.T) (*backupExport, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out", "backup.json")
	status, body := do(t, env.h, http.MethodPost, "/api/v1/backup/export", testToken, map[string]any{"path": path})
	if status != http.StatusOK {
		t.Fatalf("export status = %d (%v)", status, body)
	}
	if body["exported"] != true || body["path"] != path {
		t.Fatalf("export body = %v", body)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	var doc backupExport
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("exported file is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(raw, &doc.raw); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	return &doc, string(raw)
}

func TestExportBackupShape(t *testing.T) {
	env := newBackupEnv(t)
	env.seed(t)

	// A provider configuration row + a stored credential: neither may leak
	// into the backup.
	status, body := do(t, env.h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "deepseek", "base_url": "https://api.deepseek.com",
		"translation_model": "deepseek-flash", "chat_model": "deepseek-flash",
		"api_key": "sk-export-secret-7788",
	})
	if status != http.StatusOK {
		t.Fatalf("put provider: %d %v", status, body)
	}
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/settings", testToken, map[string]any{"theme": "dark"})
	if status != http.StatusOK {
		t.Fatalf("put settings: %d", status)
	}

	doc, raw := env.export(t)

	// Envelope exactly per schemas/backup.schema.json.
	if doc.BackupSchemaVersion != 1 {
		t.Errorf("backup_schema_version = %d", doc.BackupSchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339, doc.ExportedAt); err != nil {
		t.Errorf("exported_at %q is not RFC3339: %v", doc.ExportedAt, err)
	}
	if doc.AppVersion != backup.AppVersion {
		t.Errorf("app_version = %q, want %q", doc.AppVersion, backup.AppVersion)
	}
	for _, key := range []string{"backup_schema_version", "exported_at", "app_version", "settings",
		"translation_history", "vocabulary", "terminology", "chats", "messages", "open_tabs"} {
		if _, ok := doc.raw[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}
	for key := range doc.raw {
		switch key {
		case "backup_schema_version", "exported_at", "app_version", "settings",
			"translation_history", "vocabulary", "terminology", "chats", "messages", "open_tabs":
		default:
			t.Errorf("unexpected top-level key %q", key)
		}
	}

	// Business data counts.
	if len(doc.TranslationHistory) != 1 || doc.TranslationHistory[0]["id"] != "hist-1" {
		t.Errorf("history = %v", doc.TranslationHistory)
	}
	if h := doc.TranslationHistory[0]; h["result"] == nil || h["kind"] != "word" || h["source"] != "model" {
		t.Errorf("history entry shape = %v", h)
	}
	if len(doc.Vocabulary) != 1 || doc.Vocabulary[0]["lemma"] != "serenity" {
		t.Errorf("vocabulary = %v", doc.Vocabulary)
	}
	if w, ok := doc.Vocabulary[0]["word"].(map[string]any); !ok || w["word"] != "serenity" {
		t.Errorf("vocabulary word = %v", doc.Vocabulary[0]["word"])
	}
	if len(doc.Terminology) != 1 || doc.Terminology[0]["source"] != "LLM" {
		t.Errorf("terminology = %v", doc.Terminology)
	}
	if len(doc.Chats) != 1 || doc.Chats[0]["conversation_context"] != "ctx" {
		t.Errorf("chats = %v", doc.Chats)
	}
	if len(doc.Messages) != 1 {
		t.Fatalf("messages = %v", doc.Messages)
	}
	if m := doc.Messages[0]; m["generation_id"] != "gen-1" || m["chat_id"] != "chat-1" {
		t.Errorf("message shape = %v", m)
	}
	if len(doc.OpenTabs) != 1 || doc.OpenTabs[0]["translation_id"] != "hist-1" {
		t.Errorf("open_tabs = %v", doc.OpenTabs)
	}

	// Settings merged non-secret object, provider row excluded.
	if _, ok := doc.Settings["provider"]; ok {
		t.Errorf("settings must exclude the provider row: %v", doc.Settings)
	}
	if doc.Settings["theme"] != "dark" || doc.Settings["proxy_mode"] != "system" {
		t.Errorf("settings merge wrong: %v", doc.Settings)
	}

	// No secret anywhere in the file.
	for _, banned := range []string{"sk-export-secret-7788", "api_key", "session_token", "authorization"} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(banned)) {
			t.Errorf("backup leaks %q", banned)
		}
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	src := newBackupEnv(t)
	src.seed(t)
	doc1, _ := src.export(t)

	// Import into a fresh environment (fresh database, fresh credential).
	dst := newBackupEnv(t)
	path := filepath.Join(t.TempDir(), "backup.json")
	raw1, err := json.Marshal(doc1.raw)
	if err != nil {
		t.Fatalf("marshal source backup: %v", err)
	}
	if err := os.WriteFile(path, raw1, 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	status, body := do(t, dst.h, http.MethodPost, "/api/v1/backup/import", testToken, map[string]any{"path": path})
	if status != http.StatusOK {
		t.Fatalf("import status = %d (%v)", status, body)
	}
	imported, _ := body["imported"].(map[string]any)
	for key, want := range map[string]float64{
		"settings": 14, "translation_history": 1, "vocabulary": 1,
		"terminology": 1, "chats": 1, "messages": 1, "messages_skipped": 0, "open_tabs": 1,
	} {
		if imported[key] != want {
			t.Errorf("imported[%s] = %v, want %v", key, imported[key], want)
		}
	}

	// Export again: the business payload must be identical (only
	// exported_at differs).
	doc2, _ := dst.export(t)
	a, _ := json.Marshal(doc2.raw)
	b, _ := json.Marshal(doc1.raw)
	var ma, mb map[string]any
	_ = json.Unmarshal(a, &ma)
	_ = json.Unmarshal(b, &mb)
	delete(ma, "exported_at")
	delete(mb, "exported_at")
	cA, _ := json.Marshal(ma)
	cB, _ := json.Marshal(mb)
	if !bytes.Equal(cA, cB) {
		t.Errorf("round-trip mismatch:\nimported: %s\nsource:   %s", cA, cB)
	}
}

func TestImportBackupRejections(t *testing.T) {
	env := newBackupEnv(t)
	dir := t.TempDir()
	write := func(content string) string {
		path := filepath.Join(dir, "backup.json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return path
	}

	status, body := do(t, env.h, http.MethodPost, "/api/v1/backup/import", testToken,
		map[string]any{"path": write(`{"backup_schema_version":2,"exported_at":"x","app_version":"1","settings":{},"translation_history":[],"vocabulary":[],"terminology":[],"chats":[],"messages":[],"open_tabs":[]}`)})
	if status != http.StatusBadRequest {
		t.Fatalf("version 2 status = %d (%v)", status, body)
	}
	if code, _ := errCode(t, body); code != "BACKUP_VERSION_UNSUPPORTED" {
		t.Errorf("envelope = %v", body)
	}

	status, body = do(t, env.h, http.MethodPost, "/api/v1/backup/import", testToken,
		map[string]any{"path": write(`{"backup_schema_version":1}`)})
	if status != http.StatusBadRequest {
		t.Fatalf("missing keys status = %d", status)
	}
	if code, _ := errCode(t, body); code != "INVALID_REQUEST" {
		t.Errorf("envelope = %v", body)
	}

	status, _ = do(t, env.h, http.MethodPost, "/api/v1/backup/import", testToken,
		map[string]any{"path": filepath.Join(dir, "does-not-exist.json")})
	if status != http.StatusBadRequest {
		t.Errorf("missing file status = %d, want 400", status)
	}

	status, _ = do(t, env.h, http.MethodPost, "/api/v1/backup/import", testToken, map[string]any{"path": ""})
	if status != http.StatusBadRequest {
		t.Errorf("empty path status = %d, want 400", status)
	}
}

func TestImportMergeRules(t *testing.T) {
	env := newBackupEnv(t)
	ctx := t.Context()
	now := time.Now().UTC().Format(time.RFC3339)
	old := "2020-01-01T00:00:00Z"
	newer := "2026-01-02T00:00:00Z"

	// Existing state:
	chats := repository.NewChatsRepo(env.db)
	if err := repository.NewVocabularyRepo(env.db).Upsert(ctx, repository.VocabularyRow{
		Lemma:    "eager", // stored spelling
		WordJSON: `{"word":"eager","lemma":"eager","phonetic_uk":"","phonetic_us":"","parts_of_speech":[{"part":"adj","meanings":["旧的中文释义"]}],"synonyms":[],"inflections":[]}`,
		SavedAt:  old, LastViewedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewVocabularyRepo(env.db).Upsert(ctx, repository.VocabularyRow{
		Lemma: "fresh", WordJSON: `{"word":"fresh","lemma":"fresh","phonetic_uk":"","phonetic_us":"","parts_of_speech":[],"synonyms":[],"inflections":[]}`,
		SavedAt: now, LastViewedAt: newer, // stored entry is NEWER than backup
	}); err != nil {
		t.Fatal(err)
	}
	if s, _ := do(t, env.h, http.MethodPost, "/api/v1/terminology", testToken,
		map[string]any{"source": "CI", "target": "旧译名"}); s != http.StatusCreated {
		t.Fatal("seed terminology failed")
	}
	existingTermID := ""
	rec := httptest.NewRequest(http.MethodGet, "/api/v1/terminology", nil)
	rec.Header.Set("Authorization", "Bearer "+testToken)
	rr := httptest.NewRecorder()
	env.h.ServeHTTP(rr, rec)
	var terms []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &terms)
	for _, term := range terms {
		if term["source"] == "CI" {
			existingTermID, _ = term["id"].(string)
		}
	}
	if err := repository.NewHistoryRepo(env.db).Insert(ctx, repository.HistoryRow{
		ID: "dup-history", Kind: "word", InputText: "x", ResultJSON: `{}`, Source: "model",
		Model: "m", CreatedAt: now, LastViewedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := chats.InsertChat(ctx, repository.ChatRow{
		ID: "dup-chat", Title: "kept", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// A pre-existing message id: the backup also carries it and must be
	// ignored (insert-by-id).
	if err := chats.InsertMessage(ctx, repository.MessageRow{
		ID: "dup-msg", ChatID: "dup-chat", Role: "user", Content: "original",
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewTabsRepo(env.db).ReplaceAll(ctx, []repository.TabRow{
		{ID: "old-tab", Kind: "text", Title: "old", PayloadJSON: "{}", Position: 0, IsActive: true, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	backupDoc := map[string]any{
		"backup_schema_version": 1,
		"exported_at":           now,
		"app_version":           "1.0.0",
		"settings":              map[string]any{"theme": "light"},
		"translation_history": []any{
			map[string]any{"id": "new-history", "kind": "word", "input_text": "y", "result": map[string]any{"word": "y"}, "source": "model", "model": "m", "created_at": now},
			map[string]any{"id": "dup-history", "kind": "word", "input_text": "ignored", "result": map[string]any{}, "source": "model", "model": "m", "created_at": now},
		},
		"vocabulary": []any{
			// NEWER than stored "eager": wins with its word_json.
			map[string]any{"lemma": "EAGER", "word": map[string]any{"word": "eager", "lemma": "eager", "phonetic_uk": "", "phonetic_us": "", "parts_of_speech": []any{}, "synonyms": []any{}, "inflections": []any{}}, "saved_at": old, "last_viewed_at": newer},
			// OLDER than stored "fresh": stored row stays.
			map[string]any{"lemma": "fresh", "word": map[string]any{"word": "fresh-backup", "lemma": "fresh", "phonetic_uk": "", "phonetic_us": "", "parts_of_speech": []any{}, "synonyms": []any{}, "inflections": []any{}}, "saved_at": old, "last_viewed_at": old},
		},
		"terminology": []any{
			map[string]any{"id": "backup-term-id", "source": "ci", "target": "持续集成", "created_at": old, "updated_at": now},
			map[string]any{"id": "new-term", "source": "SDK", "target": "软件开发工具包", "created_at": now, "updated_at": now},
		},
		"chats": []any{
			map[string]any{"id": "new-chat", "title": "from backup", "conversation_context": "", "compact_summary": "", "created_at": now, "updated_at": now},
			map[string]any{"id": "dup-chat", "title": "ignored", "conversation_context": "", "compact_summary": "", "created_at": now, "updated_at": now},
		},
		"messages": []any{
			map[string]any{"id": "new-msg", "chat_id": "new-chat", "role": "user", "content": "hello", "reasoning_content": "", "created_at": now, "generation_id": nil},
			map[string]any{"id": "dup-msg", "chat_id": "dup-chat", "role": "user", "content": "dup id", "reasoning_content": "", "created_at": now, "generation_id": nil},
			map[string]any{"id": "orphan-msg", "chat_id": "ghost-chat", "role": "user", "content": "orphan", "reasoning_content": "", "created_at": now, "generation_id": nil},
		},
		"open_tabs": []any{
			map[string]any{"id": "tab-b", "kind": "text", "title": "B", "translation_id": nil, "payload": map[string]any{}, "position": 5, "is_active": true, "updated_at": now},
			map[string]any{"id": "tab-a", "kind": "word", "title": "A", "translation_id": nil, "payload": map[string]any{}, "position": 9, "is_active": true, "updated_at": now},
			map[string]any{"id": "tab-c", "kind": "word", "title": "C", "translation_id": nil, "payload": map[string]any{}, "position": 1, "is_active": false, "updated_at": now},
		},
	}
	path := filepath.Join(t.TempDir(), "merge.json")
	raw, _ := json.Marshal(backupDoc)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	status, body := do(t, env.h, http.MethodPost, "/api/v1/backup/import", testToken, map[string]any{"path": path})
	if status != http.StatusOK {
		t.Fatalf("import status = %d (%v)", status, body)
	}
	imported, _ := body["imported"].(map[string]any)
	want := map[string]float64{
		"settings": 1, "translation_history": 1, "vocabulary": 1, "terminology": 2,
		"chats": 1, "messages": 1, "messages_skipped": 1, "open_tabs": 3,
	}
	for key, w := range want {
		if imported[key] != w {
			t.Errorf("imported[%s] = %v, want %v", key, imported[key], w)
		}
	}

	// Vocabulary: EAGER (newer backup) replaced word_json; fresh kept its
	// newer stored row.
	list := env.vocabularyList(t)
	byLemma := map[string]map[string]any{}
	for _, item := range list {
		byLemma[item["lemma"].(string)] = item
	}
	if len(list) != 2 {
		t.Fatalf("vocabulary items = %d, want 2 (NOCASE dedupe)", len(list))
	}
	if w, _ := byLemma["eager"]["word"].(map[string]any); w == nil || w["lemma"] != "eager" {
		// backup row wins via last_viewed_at; word_json comes from the backup.
		if got := byLemma["eager"]; got == nil {
			t.Fatalf("eager missing: %v", list)
		}
	}
	if byLemma["eager"]["last_viewed_at"] != newer {
		t.Errorf("eager last_viewed_at = %v, want backup's %v", byLemma["eager"]["last_viewed_at"], newer)
	}
	if byLemma["eager"]["saved_at"] != old {
		t.Errorf("eager saved_at = %v, want original %v", byLemma["eager"]["saved_at"], old)
	}
	if w, _ := byLemma["fresh"]["word"].(map[string]any); w["word"] != "fresh" {
		t.Errorf("fresh must keep the newer stored word_json: %v", byLemma["fresh"])
	}

	// Terminology: backup overwrote the existing CI row keeping its id, and
	// added SDK with the backup id (the only pre-existing rows were CI).
	rr = httptest.NewRecorder()
	rec = httptest.NewRequest(http.MethodGet, "/api/v1/terminology", nil)
	rec.Header.Set("Authorization", "Bearer "+testToken)
	env.h.ServeHTTP(rr, rec)
	terms = nil
	_ = json.Unmarshal(rr.Body.Bytes(), &terms)
	bySource := map[string]map[string]any{}
	for _, term := range terms {
		bySource[strings.ToLower(term["source"].(string))] = term
	}
	if len(terms) != 2 {
		t.Fatalf("terminology rows = %d, want 2", len(terms))
	}
	if bySource["ci"]["target"] != "持续集成" || bySource["ci"]["id"] != existingTermID {
		t.Errorf("CI row = %v, want target overwritten with id %s kept", bySource["ci"], existingTermID)
	}
	if bySource["sdk"]["id"] != "new-term" || bySource["sdk"]["target"] != "软件开发工具包" {
		t.Errorf("SDK row = %v", bySource["sdk"])
	}

	// History: dup ignored, new inserted.
	rows, _, err := repository.NewHistoryRepo(env.db).List(ctx, repository.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "dup-history" && rows[1].ID != "dup-history" {
		t.Fatalf("history rows = %+v", rows)
	}
	for _, row := range rows {
		if row.ID == "dup-history" && row.InputText != "x" {
			t.Errorf("dup history was overwritten: %+v", row)
		}
	}

	// Chats/messages: dup chat kept with its original message (the backup's
	// duplicate id ignored), orphan skipped, new message inserted.
	if n, _ := chats.CountMessages(ctx, "dup-chat"); n != 1 {
		t.Errorf("dup-chat must keep its original message, has %d", n)
	}
	dupMsgs, _ := chats.ListMessages(ctx, "dup-chat")
	if len(dupMsgs) == 1 && dupMsgs[0].Content != "original" {
		t.Errorf("dup-msg must not be overwritten: %+v", dupMsgs[0])
	}
	msgs, err := chats.ListMessages(ctx, "new-chat")
	if err != nil || len(msgs) != 1 || msgs[0].ID != "new-msg" {
		t.Fatalf("new-chat messages = %+v err=%v", msgs, err)
	}
	if _, err := chats.GetChat(ctx, "dup-chat"); err != nil {
		t.Errorf("dup-chat must still exist: %v", err)
	}
	dupChat, _ := chats.GetChat(ctx, "dup-chat")
	if dupChat.Title != "kept" {
		t.Errorf("dup chat was overwritten: %+v", dupChat)
	}

	// Tabs: full replace, positions 0..2, exactly one active (first).
	env.assertTabs(t, []string{"tab-b", "tab-a", "tab-c"}, "tab-b")
}

func (env *backupEnv) vocabularyList(t *testing.T) []map[string]any {
	t.Helper()
	rec := httptest.NewRequest(http.MethodGet, "/api/v1/vocabulary", nil)
	rec.Header.Set("Authorization", "Bearer "+testToken)
	rr := httptest.NewRecorder()
	env.h.ServeHTTP(rr, rec)
	if rr.Code != http.StatusOK {
		t.Fatalf("vocabulary list status = %d (%s)", rr.Code, rr.Body.String())
	}
	var items []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("vocabulary response: %v", err)
	}
	return items
}

// assertTabs fetches GET /tabs and checks order, positions and the single
// active tab. An empty wantIDs expects no tabs at all.
func (env *backupEnv) assertTabs(t *testing.T, wantIDs []string, wantActive string) {
	t.Helper()
	rec := httptest.NewRequest(http.MethodGet, "/api/v1/tabs", nil)
	rec.Header.Set("Authorization", "Bearer "+testToken)
	rr := httptest.NewRecorder()
	env.h.ServeHTTP(rr, rec)
	var tabs []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &tabs); err != nil {
		t.Fatalf("tabs response: %v (%s)", err, rr.Body.String())
	}
	if len(wantIDs) == 0 {
		if len(tabs) != 0 {
			t.Fatalf("tabs = %v, want none", tabs)
		}
		return
	}
	if len(tabs) != len(wantIDs) {
		t.Fatalf("tabs = %d, want %d", len(tabs), len(wantIDs))
	}
	activeCount := 0
	for i, tab := range tabs {
		if tab["id"] != wantIDs[i] {
			t.Errorf("tabs[%d].id = %v, want %v", i, tab["id"], wantIDs[i])
		}
		if tab["position"] != float64(i) {
			t.Errorf("tabs[%d].position = %v, want %d", i, tab["position"], i)
		}
		if tab["is_active"] == true {
			activeCount++
			if tab["id"] != wantActive {
				t.Errorf("active tab = %v, want %v", tab["id"], wantActive)
			}
		}
	}
	if activeCount != 1 {
		t.Errorf("active tabs = %d, want exactly 1", activeCount)
	}
}

func TestClearBusinessAndResetApp(t *testing.T) {
	env := newBackupEnv(t)
	env.seed(t)
	do(t, env.h, http.MethodPut, "/api/v1/settings", testToken, map[string]any{"theme": "dark", "proxy_mode": "none"})
	do(t, env.h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "deepseek", "base_url": "https://api.deepseek.com",
		"translation_model": "deepseek-flash", "chat_model": "deepseek-flash",
		"api_key": "sk-clear-secret-4242",
	})

	status, _ := do(t, env.h, http.MethodPost, "/api/v1/data/clear-business", testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("clear-business status = %d", status)
	}

	// Business data gone.
	if items := env.vocabularyList(t); len(items) != 0 {
		t.Errorf("vocabulary after clear = %d", len(items))
	}
	env.assertTabs(t, nil, "")
	rows, _, err := repository.NewHistoryRepo(env.db).List(t.Context(), repository.ListParams{})
	if err != nil || len(rows) != 0 {
		t.Errorf("history after clear = %d err=%v", len(rows), err)
	}
	var chatCount, cacheCount int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&chatCount); err != nil || chatCount != 0 {
		t.Errorf("chats after clear = %d err=%v", chatCount, err)
	}
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&chatCount); err != nil || chatCount != 0 {
		t.Errorf("messages after clear = %d err=%v", chatCount, err)
	}
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM translation_cache`).Scan(&cacheCount); err != nil || cacheCount != 0 {
		t.Errorf("cache after clear = %d err=%v", cacheCount, err)
	}

	// Settings and credential preserved.
	_, settings := do(t, env.h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if settings["theme"] != "dark" || settings["proxy_mode"] != "none" {
		t.Errorf("settings must survive clear-business: %v", settings)
	}
	_, provider := do(t, env.h, http.MethodGet, "/api/v1/settings/provider", testToken, nil)
	if provider["api_key_configured"] != true {
		t.Errorf("credential must survive clear-business: %v", provider)
	}

	// Reset app: settings back to defaults, credential deleted.
	status, _ = do(t, env.h, http.MethodPost, "/api/v1/data/reset-app", testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("reset-app status = %d", status)
	}
	_, settings = do(t, env.h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if settings["theme"] != "system" || settings["proxy_mode"] != "system" {
		t.Errorf("settings must reset to defaults: %v", settings)
	}
	_, provider = do(t, env.h, http.MethodGet, "/api/v1/settings/provider", testToken, nil)
	if provider["api_key_configured"] != false || provider["api_key_hint"] != "" {
		t.Errorf("credential must be deleted by reset-app: %v", provider)
	}
	if _, found, _ := env.cred.Load(); found {
		t.Error("credential store still holds the key after reset-app")
	}
}

func TestCredentialBackedProviderSettings(t *testing.T) {
	env := newBackupEnv(t)

	_, provider := do(t, env.h, http.MethodGet, "/api/v1/settings/provider", testToken, nil)
	if provider["api_key_configured"] != false {
		t.Fatalf("fresh store must be unconfigured: %v", provider)
	}

	// Save a key.
	status, body := do(t, env.h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "deepseek", "base_url": "https://api.deepseek.com",
		"translation_model": "deepseek-flash", "chat_model": "deepseek-flash",
		"api_key": "sk-abcxyz-987654",
	})
	if status != http.StatusOK {
		t.Fatalf("put provider: %d %v", status, body)
	}
	if body["api_key_configured"] != true {
		t.Errorf("configured = %v", body["api_key_configured"])
	}
	if hint, _ := body["api_key_hint"].(string); hint != "sk-…" {
		t.Errorf("hint = %q, want sk-…", hint)
	}

	// Short keys: hint is just the ellipsis.
	status, body = do(t, env.h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "deepseek", "base_url": "https://api.deepseek.com",
		"translation_model": "m", "chat_model": "m", "api_key": "short",
	})
	if status != http.StatusOK || body["api_key_hint"] != "…" {
		t.Errorf("short key hint = %v (%v)", body["api_key_hint"], body)
	}

	// Empty/absent api_key is a no-op: the key stays configured.
	status, body = do(t, env.h, http.MethodPut, "/api/v1/settings/provider", testToken, map[string]any{
		"mode": "deepseek", "base_url": "https://api.deepseek.com",
		"translation_model": "m", "chat_model": "m",
	})
	if status != http.StatusOK || body["api_key_configured"] != true {
		t.Errorf("empty api_key must not delete the credential: %v", body)
	}

	// A backend restart (new store, same credential store) keeps the key.
	db, err := database.Open(filepath.Join(t.TempDir(), "second.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatal(err)
	}
	settingsRepo := repository.NewSettingsRepo(db)
	restarted := NewProviderSettingsStore(settingsRepo, env.cred, logging.NewRedactor())
	view, err := restarted.View(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !view.APIKeyConfigured || view.APIKeyHint != "…" {
		t.Errorf("restarted view = %+v", view)
	}
}

func TestPutSettingsProxyValidation(t *testing.T) {
	env := newBackupEnv(t)

	// socks5 without a URL → 400.
	status, body := do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"proxy_mode": "socks5"})
	if status != http.StatusBadRequest {
		t.Fatalf("socks5 without url status = %d (%v)", status, body)
	}
	if code, _ := errCode(t, body); code != "INVALID_REQUEST" {
		t.Errorf("envelope = %v", body)
	}

	// Scheme mismatch → 400.
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"proxy_mode": "http", "proxy_url": "socks5://127.0.0.1:1080"})
	if status != http.StatusBadRequest {
		t.Errorf("scheme mismatch status = %d, want 400", status)
	}

	// Unparseable URL → 400.
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"proxy_mode": "https", "proxy_url": "not a url"})
	if status != http.StatusBadRequest {
		t.Errorf("bad url status = %d, want 400", status)
	}

	// Unknown mode → 400.
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"proxy_mode": "ftp", "proxy_url": "ftp://x"})
	if status != http.StatusBadRequest {
		t.Errorf("unknown mode status = %d, want 400", status)
	}

	// Invalid request must not have stored anything.
	_, settings := do(t, env.h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if settings["proxy_mode"] != "system" || settings["proxy_url"] != "" {
		t.Errorf("rejected PUT must not change stored values: %v", settings)
	}

	// Valid pairs persist.
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"proxy_mode": "socks5", "proxy_url": "socks5://127.0.0.1:1080"})
	if status != http.StatusOK {
		t.Fatalf("valid socks5 status = %d", status)
	}
	_, settings = do(t, env.h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if settings["proxy_mode"] != "socks5" || settings["proxy_url"] != "socks5://127.0.0.1:1080" {
		t.Errorf("socks5 settings = %v", settings)
	}

	// Legacy default (system) and direct (none) still accepted.
	status, _ = do(t, env.h, http.MethodPut, "/api/v1/settings", testToken,
		map[string]any{"proxy_mode": "none"})
	if status != http.StatusOK {
		t.Fatalf("none mode status = %d", status)
	}
	_, settings = do(t, env.h, http.MethodGet, "/api/v1/settings", testToken, nil)
	if settings["proxy_mode"] != "none" {
		t.Errorf("none mode = %v", settings)
	}
}
