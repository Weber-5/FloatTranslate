package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
)

func newRepoDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "repo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func ts(offsetMinutes int) string {
	return time.Now().UTC().Add(time.Duration(offsetMinutes) * time.Minute).Format(time.RFC3339)
}

func TestHistoryCRUDAndCursorPagination(t *testing.T) {
	db := newRepoDB(t)
	repo := NewHistoryRepo(db)
	ctx := context.Background()

	stamps := []string{ts(0), ts(1), ts(2), ts(3), ts(4)}
	for i := 0; i < 5; i++ {
		row := HistoryRow{
			ID:             string(rune('a'+i)) + "123456789012345678901234",
			Kind:           "word",
			InputText:      "input word",
			NormalizedText: "input word",
			ResultJSON:     `{"word":"x"}`,
			Source:         "model",
			Model:          "deepseek-flash",
			CreatedAt:      stamps[i],
			LastViewedAt:   stamps[i],
		}
		if i == 1 {
			row.Kind = "text"
			row.InputText = "special query needle here"
		}
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Page 1: limit 2, newest first.
	rows, next, err := repo.List(ctx, ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || next == "" {
		t.Fatalf("page1 rows=%d next=%q", len(rows), next)
	}
	if rows[0].CreatedAt != stamps[4] || rows[1].CreatedAt != stamps[3] {
		t.Errorf("page1 order wrong: %s, %s", rows[0].CreatedAt, rows[1].CreatedAt)
	}

	// Page 2 via cursor.
	rows, next, err = repo.List(ctx, ListParams{Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(rows) != 2 || next == "" {
		t.Fatalf("page2 rows=%d next=%q", len(rows), next)
	}
	// Page 3: one row, no next cursor.
	rows, next, err = repo.List(ctx, ListParams{Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("list page3: %v", err)
	}
	if len(rows) != 1 || next != "" {
		t.Fatalf("page3 rows=%d next=%q", len(rows), next)
	}

	// Kind filter.
	rows, _, err = repo.List(ctx, ListParams{Kind: "text"})
	if err != nil {
		t.Fatalf("list kind: %v", err)
	}
	if len(rows) != 1 || rows[0].Kind != "text" {
		t.Errorf("kind filter rows=%d", len(rows))
	}

	// Query filter.
	rows, _, err = repo.List(ctx, ListParams{Query: "needle"})
	if err != nil {
		t.Fatalf("list query: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("query filter rows=%d, want 1", len(rows))
	}

	// Get / UpdateResult / Delete.
	got, err := repo.Get(ctx, "a123456789012345678901234")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.InputText != "input word" {
		t.Errorf("get input_text = %q", got.InputText)
	}
	if err := repo.UpdateResult(ctx, got.ID, HistoryRow{
		ResultJSON: `{"word":"y"}`, Source: "cache", Model: "m2",
		CreatedAt: ts(99), LastViewedAt: ts(99),
	}); err != nil {
		t.Fatalf("update result: %v", err)
	}
	got, _ = repo.Get(ctx, got.ID)
	if got.ResultJSON != `{"word":"y"}` || got.Source != "cache" || got.CreatedAt != ts(99) {
		t.Errorf("update result not persisted: %+v", got)
	}
	if err := repo.Delete(ctx, got.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, got.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	if err := repo.Delete(ctx, "missing"); err != ErrNotFound {
		t.Errorf("delete missing should be ErrNotFound, got %v", err)
	}

	if err := repo.DeleteAll(ctx); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	rows, _, _ = repo.List(ctx, ListParams{})
	if len(rows) != 0 {
		t.Errorf("history not cleared: %d rows", len(rows))
	}
}

func TestVocabularyDedupeByLemmaNOCASE(t *testing.T) {
	db := newRepoDB(t)
	repo := NewVocabularyRepo(db)
	ctx := context.Background()

	savedAt, viewedAt := ts(0), ts(5)
	first := VocabularyRow{Lemma: "Run", WordJSON: `{"word":"Run"}`, SavedAt: savedAt, LastViewedAt: savedAt}
	if err := repo.Upsert(ctx, first); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	// Same lemma, different case, later last_viewed_at: dedupe, refresh view.
	second := VocabularyRow{Lemma: "run", WordJSON: `{"word":"run"}`, SavedAt: viewedAt, LastViewedAt: viewedAt}
	if err := repo.Upsert(ctx, second); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	rows, err := repo.List(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("vocabulary rows = %d, want 1 (lemma dedupe)", len(rows))
	}
	if rows[0].SavedAt != savedAt {
		t.Errorf("saved_at changed on duplicate save: %s", rows[0].SavedAt)
	}
	if rows[0].LastViewedAt != viewedAt {
		t.Errorf("last_viewed_at not refreshed: %s", rows[0].LastViewedAt)
	}

	got, err := repo.Get(ctx, "RUN")
	if err != nil {
		t.Fatalf("get by uppercase: %v", err)
	}
	if got.Lemma != "Run" {
		t.Errorf("get lemma = %q", got.Lemma)
	}

	if err := repo.Delete(ctx, "run"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, "Run"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestChatCascadeDelete(t *testing.T) {
	db := newRepoDB(t)
	repo := NewChatsRepo(db)
	ctx := context.Background()

	chat := ChatRow{ID: "chat-1", Title: "session", CreatedAt: ts(0), UpdatedAt: ts(0)}
	if err := repo.InsertChat(ctx, chat); err != nil {
		t.Fatalf("insert chat: %v", err)
	}
	for i := 0; i < 2; i++ {
		msg := MessageRow{
			ID: string(rune('a'+i)) + "-msg", ChatID: "chat-1", Role: "user",
			Content: "hello", CreatedAt: ts(i),
		}
		if err := repo.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
	n, err := repo.CountMessages(ctx, "chat-1")
	if err != nil || n != 2 {
		t.Fatalf("messages before delete = %d err=%v", n, err)
	}
	if err := repo.DeleteChat(ctx, "chat-1"); err != nil {
		t.Fatalf("delete chat: %v", err)
	}
	n, err = repo.CountMessages(ctx, "chat-1")
	if err != nil || n != 0 {
		t.Errorf("messages must cascade-delete, got %d err=%v", n, err)
	}
	if _, err := repo.GetChat(ctx, "chat-1"); err != ErrNotFound {
		t.Errorf("chat should be gone, got %v", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	db := newRepoDB(t)
	repo := NewSettingsRepo(db)
	ctx := context.Background()

	if _, err := repo.Get(ctx, "theme"); err != ErrNotFound {
		t.Errorf("missing key should be ErrNotFound, got %v", err)
	}
	if err := repo.Put(ctx, "theme", `"dark"`); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := repo.Put(ctx, "theme", `"light"`); err != nil {
		t.Fatalf("put 2: %v", err)
	}
	v, err := repo.Get(ctx, "theme")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v != `"light"` {
		t.Errorf("settings round-trip value = %q", v)
	}
	rows, err := repo.List(ctx)
	if err != nil || len(rows) != 1 || rows[0].Key != "theme" {
		t.Errorf("list = %+v err=%v", rows, err)
	}
}

func TestTerminologyUniqueSourceNOCASE(t *testing.T) {
	db := newRepoDB(t)
	repo := NewTerminologyRepo(db)
	ctx := context.Background()

	row := TerminologyRow{ID: "t-1", Source: "API", Target: "接口", CreatedAt: ts(0), UpdatedAt: ts(0)}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("create: %v", err)
	}
	dup := TerminologyRow{ID: "t-2", Source: "api", Target: "另一个", CreatedAt: ts(1), UpdatedAt: ts(1)}
	if err := repo.Create(ctx, dup); err != ErrConflict {
		t.Errorf("duplicate source (NOCASE) should be ErrConflict, got %v", err)
	}
	if err := repo.Update(ctx, TerminologyRow{ID: "t-1", Source: "Api", Target: "应用程序接口", UpdatedAt: ts(2)}); err != nil {
		t.Errorf("updating own row with same source must not conflict: %v", err)
	}
	if err := repo.Update(ctx, TerminologyRow{ID: "t-1", Source: "HTTP", Target: "协议", UpdatedAt: ts(3)}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := repo.Get(ctx, "t-1")
	if got.Source != "HTTP" || got.Target != "协议" {
		t.Errorf("update not persisted: %+v", got)
	}
	if err := repo.Delete(ctx, "t-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.Delete(ctx, "t-1"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCacheRepoRoundTrip(t *testing.T) {
	db := newRepoDB(t)
	repo := NewCacheRepo(db)
	ctx := context.Background()

	row := CacheRow{
		CacheKey: "abc", Kind: "word", NormalizedInput: "run",
		Model: "deepseek-flash", ConfigHash: "hash", ResultJSON: `{"word":"run"}`,
		CreatedAt: ts(0), UpdatedAt: ts(0), LastUsedAt: ts(0),
	}
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Overwrite (bypass_cache path) must replace the result.
	row.ResultJSON = `{"word":"run again"}`
	row.UpdatedAt = ts(1)
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	got, err := repo.Get(ctx, "abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ResultJSON != `{"word":"run again"}` {
		t.Errorf("cache overwrite failed: %s", got.ResultJSON)
	}
	if err := repo.Touch(ctx, "abc", ts(9)); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, _ = repo.Get(ctx, "abc")
	if got.LastUsedAt != ts(9) {
		t.Errorf("touch failed: %s", got.LastUsedAt)
	}
	if _, err := repo.Get(ctx, "missing"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTabsReplaceAllAndList(t *testing.T) {
	db := newRepoDB(t)
	repo := NewTabsRepo(db)
	ctx := context.Background()

	rows := []TabRow{
		{ID: "tab-b", Kind: "text", Title: "second", PayloadJSON: `{"k":1}`, Position: 1, IsActive: true, UpdatedAt: ts(0)},
		{ID: "tab-a", Kind: "word", Title: "first", PayloadJSON: `{}`, Position: 0, UpdatedAt: ts(0)},
	}
	if err := repo.ReplaceAll(ctx, rows); err != nil {
		t.Fatalf("replace all: %v", err)
	}
	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tabs = %d, want 2", len(got))
	}
	if got[0].ID != "tab-a" || got[1].ID != "tab-b" {
		t.Errorf("tabs not ordered by position: %s, %s", got[0].ID, got[1].ID)
	}
	if got[1].TranslationID.Valid {
		t.Errorf("translation_id should be null")
	}
	// Full replace removes previously stored tabs.
	if err := repo.ReplaceAll(ctx, rows[:1]); err != nil {
		t.Fatalf("replace all 2: %v", err)
	}
	got, _ = repo.List(ctx)
	if len(got) != 1 || got[0].ID != "tab-b" {
		t.Errorf("full replace failed: %+v", got)
	}
}
