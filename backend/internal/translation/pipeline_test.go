package translation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/nlp"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/terminology"
)

type harness struct {
	pipeline *Pipeline
	provider *MockProvider
	db       *sql.DB
	cache    *repository.CacheRepo
	history  *repository.HistoryRepo
	terms    *terminology.Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "pipeline.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cache := repository.NewCacheRepo(db)
	history := repository.NewHistoryRepo(db)
	terms, err := terminology.NewService(context.Background(), repository.NewTerminologyRepo(db))
	if err != nil {
		t.Fatalf("terminology service: %v", err)
	}
	provider := NewMockProvider()
	pipeline, err := NewPipeline(provider, func() string { return DefaultTranslationModel },
		terms, cache, history)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	return &harness{pipeline: pipeline, provider: provider, db: db, cache: cache, history: history, terms: terms}
}

func code(t *testing.T, err error) (apperr.Code, bool) {
	t.Helper()
	if err == nil {
		return "", false
	}
	e := apperr.AsE(err)
	return e.Code, e.Retryable
}

func countHistory(t *testing.T, h *harness) int {
	t.Helper()
	rows, _, err := h.history.List(context.Background(), repository.ListParams{Limit: 200})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	return len(rows)
}

func TestWordPipelineProducesSchemaValidWordResult(t *testing.T) {
	h := newHarness(t)
	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "suspended"}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Kind != nlp.KindWord {
		t.Errorf("kind = %s, want word", resp.Kind)
	}
	if resp.Source != SourceModel {
		t.Errorf("source = %s, want model", resp.Source)
	}
	if resp.Model != DefaultTranslationModel {
		t.Errorf("model = %s", resp.Model)
	}
	if len(resp.TranslationID) != 26 {
		t.Errorf("translation_id %q is not a 26-char ULID", resp.TranslationID)
	}
	if _, err := h.history.Get(context.Background(), resp.TranslationID); err != nil {
		t.Errorf("translation_id must be persisted as history id: %v", err)
	}
	word, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", resp.Result)
	}
	if word["lemma"] != "suspend" {
		t.Errorf("mock lemma = %v, want suspend", word["lemma"])
	}
	if resp.CreatedAt == "" || !strings.HasSuffix(resp.CreatedAt, "Z") {
		t.Errorf("created_at must be RFC3339 UTC, got %q", resp.CreatedAt)
	}
}

func TestTextPipelineClassification(t *testing.T) {
	h := newHarness(t)
	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "Hello, world! This is a sentence."}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Kind != nlp.KindText {
		t.Errorf("kind = %s, want text", resp.Kind)
	}
	text, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object")
	}
	if _, ok := text["translated_markdown"]; !ok {
		t.Errorf("text result missing translated_markdown")
	}
}

func TestForceKindAndInvalidForceKind(t *testing.T) {
	h := newHarness(t)
	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "suspended", ForceKind: "text"}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Kind != nlp.KindText {
		t.Errorf("force_kind=text must win, got %s", resp.Kind)
	}

	_, err = h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "suspended", ForceKind: "phrase"}, Options{})
	if c, _ := code(t, err); c != apperr.CodeInvalidRequest {
		t.Errorf("invalid force_kind should be INVALID_REQUEST, got %v", err)
	}
}

func TestEmptyInputInvalidRequest(t *testing.T) {
	h := newHarness(t)
	_, err := h.pipeline.Translate(context.Background(), dto.TranslationRequest{Text: "   "}, Options{})
	if c, _ := code(t, err); c != apperr.CodeInvalidRequest {
		t.Errorf("empty input should be INVALID_REQUEST, got %v", err)
	}
}

func TestUnsupportedLanguage(t *testing.T) {
	h := newHarness(t)
	_, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "这是一段纯中文的输入内容"}, Options{})
	if err == nil {
		t.Fatal("expected UNSUPPORTED_LANGUAGE error")
	}
	c, retryable := code(t, err)
	if c != apperr.CodeUnsupportedLanguage {
		t.Errorf("code = %s, want UNSUPPORTED_LANGUAGE", c)
	}
	if retryable {
		t.Errorf("UNSUPPORTED_LANGUAGE must not be retryable")
	}
	if !strings.Contains(apperr.AsE(err).Message, "暂仅支持英译中") {
		t.Errorf("message must mention 暂仅支持英译中, got %q", apperr.AsE(err).Message)
	}
}

func TestCacheHitSkipsProvider(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{})
	if err != nil {
		t.Fatalf("first translate: %v", err)
	}
	if h.provider.Calls() != 1 {
		t.Fatalf("provider calls after first = %d, want 1", h.provider.Calls())
	}
	second, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{})
	if err != nil {
		t.Fatalf("second translate: %v", err)
	}
	if h.provider.Calls() != 1 {
		t.Errorf("provider must NOT be called on cache hit, calls=%d", h.provider.Calls())
	}
	if second.Source != SourceCache {
		t.Errorf("second source = %s, want cache", second.Source)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("cache hit should keep result creation time")
	}
	// Both requests are recorded in history.
	if n := countHistory(t, h); n != 2 {
		t.Errorf("history rows = %d, want 2", n)
	}
	if second.TranslationID == first.TranslationID {
		t.Errorf("cache hit must create a distinct history row id")
	}
}

func TestBypassCacheCallsProviderAndOverwrites(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{}); err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended", BypassCache: true}, Options{})
	if err != nil {
		t.Fatalf("bypass translate: %v", err)
	}
	if h.provider.Calls() != 2 {
		t.Errorf("bypass_cache must call the provider again, calls=%d", h.provider.Calls())
	}
	if second.Source != SourceModel {
		t.Errorf("bypass result source = %s, want model", second.Source)
	}
}

func TestFirstInvalidRepairedSecondInvalidFails(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// First response invalid, repair succeeds.
	h.provider.InjectInvalidResponses(1)
	resp, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{})
	if err != nil {
		t.Fatalf("repair should succeed: %v", err)
	}
	if resp.Source != SourceModel {
		t.Errorf("repaired result source = %s", resp.Source)
	}
	if h.provider.Calls() != 2 {
		t.Errorf("expected original + repair call, calls=%d", h.provider.Calls())
	}

	// Both responses invalid → STRUCTURED_OUTPUT_INVALID, not retryable.
	h2 := newHarness(t)
	h2.provider.InjectInvalidResponses(2)
	_, err = h2.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{})
	if err == nil {
		t.Fatal("expected STRUCTURED_OUTPUT_INVALID")
	}
	if c, retryable := code(t, err); c != apperr.CodeStructuredOutputInvalid || retryable {
		t.Errorf("want STRUCTURED_OUTPUT_INVALID non-retryable, got %s retryable=%v", c, retryable)
	}
	if h2.provider.Calls() != 2 {
		t.Errorf("exactly one repair attempt allowed, calls=%d", h2.provider.Calls())
	}
}

func TestProviderFailureWithCacheReturnsFlaggedCache(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{}); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	h.provider.InjectFailures(1)
	resp, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended", BypassCache: true}, Options{})
	if err != nil {
		t.Fatalf("provider failure with cache should return 200 cache result: %v", err)
	}
	if resp.Source != SourceCache {
		t.Errorf("fallback source = %s, want cache (flagged)", resp.Source)
	}
	if _, ok := resp.Result.(map[string]any); !ok {
		t.Errorf("fallback result missing")
	}
}

func TestProviderFailureWithoutCacheIsRetryable(t *testing.T) {
	h := newHarness(t)
	h.provider.InjectFailures(1)
	_, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "suspended"}, Options{})
	if err == nil {
		t.Fatal("expected provider error")
	}
	if c, retryable := code(t, err); c != apperr.CodeProviderConnectionFailed || !retryable {
		t.Errorf("want PROVIDER_CONNECTION_FAILED retryable, got %s retryable=%v", c, retryable)
	}
}

func TestTerminologyAppliedToPrompt(t *testing.T) {
	h := newHarness(t)
	if _, err := h.terms.Create(context.Background(), "FloatTranslate", "浮动翻译"); err != nil {
		t.Fatalf("create term: %v", err)
	}
	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "FloatTranslate is great", ForceKind: "text"}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	// The mock echoes its input back into the text result; it must contain
	// the terminology replacement, not the raw source term.
	text := resp.Result.(map[string]any)
	source := text["source_markdown"].(string)
	if strings.Contains(source, "FloatTranslate is great") {
		t.Errorf("terminology was not applied: %q", source)
	}
	if !strings.Contains(source, "浮动翻译 is great") {
		t.Errorf("expected terminology replacement in: %q", source)
	}
}

func TestCacheKeyFormula(t *testing.T) {
	configHash := ConfigHash("deepseek-flash", nil)
	want := sha256.Sum256([]byte("hello\x1fword\x1fdeepseek-flash\x1f" + configHash))
	got := CacheKey("hello", "word", "deepseek-flash", configHash)
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("CacheKey formula mismatch: %s", got)
	}

	// Config hash must depend on the terminology snapshot.
	terms := []repository.TerminologyRow{{Source: "API", Target: "接口"}}
	if ConfigHash("deepseek-flash", nil) == ConfigHash("deepseek-flash", terms) {
		t.Errorf("config hash must change with terminology snapshot")
	}
	if ConfigHash("model-a", nil) == ConfigHash("model-b", nil) {
		t.Errorf("config hash must change with model")
	}
}

func TestResultJSONRoundTripsThroughCache(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{}); err != nil {
		t.Fatalf("translate: %v", err)
	}
	cached, err := h.cache.Get(ctx, CacheKey("suspended", nlp.KindWord, DefaultTranslationModel,
		ConfigHash(DefaultTranslationModel, nil)))
	if err != nil {
		t.Fatalf("cache row missing: %v", err)
	}
	var stored dto.WordTranslation
	if err := json.Unmarshal([]byte(cached.ResultJSON), &stored); err != nil {
		t.Fatalf("cached result invalid: %v", err)
	}
	if stored.Lemma != "suspend" {
		t.Errorf("cached lemma = %q", stored.Lemma)
	}
}
