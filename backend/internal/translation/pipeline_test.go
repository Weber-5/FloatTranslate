package translation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/nlp"
	"github.com/Weber-5/FloatTranslate/backend/internal/protectedspan"
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
	settings *repository.SettingsRepo
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
	settings := repository.NewSettingsRepo(db)
	terms, err := terminology.NewService(context.Background(), repository.NewTerminologyRepo(db))
	if err != nil {
		t.Fatalf("terminology service: %v", err)
	}
	provider := NewMockProvider()
	pipeline, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		terms, cache, history, settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	return &harness{pipeline: pipeline, provider: provider, db: db, cache: cache,
		history: history, terms: terms, settings: settings}
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
	// source_markdown is the original input unchanged; the terminology
	// replacement must appear in the translated surface (the mock echoes its
	// terminology-applied input back into the translation).
	text := resp.Result.(map[string]any)
	if text["source_markdown"] != "FloatTranslate is great" {
		t.Errorf("source_markdown = %v, want the original input unchanged", text["source_markdown"])
	}
	translated := text["translated_markdown"].(string)
	if strings.Contains(translated, "FloatTranslate is great") {
		t.Errorf("terminology was not applied in translation: %q", translated)
	}
	if !strings.Contains(translated, "浮动翻译 is great") {
		t.Errorf("expected terminology replacement in: %q", translated)
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

// --- Phase 2: scripted provider, protected spans, terminology, resolver ---

// scriptStep is one scripted provider response.
type scriptStep struct {
	content string
	err     error
}

// scriptProvider plays back a scripted sequence of responses and records the
// requests it received. When the script is exhausted it repeats the last
// step; with no script it fails.
type scriptProvider struct {
	mu    sync.Mutex
	steps []scriptStep
	i     int
	reqs  []llm.CompleteRequest
}

func newScriptProvider(steps ...scriptStep) *scriptProvider {
	return &scriptProvider{steps: steps}
}

func (s *scriptProvider) Complete(_ context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	if len(s.steps) == 0 {
		return llm.CompleteResponse{}, llm.ErrUnavailable
	}
	i := s.i
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	s.i++
	step := s.steps[i]
	if step.err != nil {
		return llm.CompleteResponse{}, step.err
	}
	return llm.CompleteResponse{Content: step.content}, nil
}

func (s *scriptProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsThinking: true, SupportsStructuredOutput: true}
}

func (s *scriptProvider) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

func (s *scriptProvider) requests() []llm.CompleteRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]llm.CompleteRequest, len(s.reqs))
	copy(out, s.reqs)
	return out
}

// textPayload builds a schema-valid per-chunk text payload (Phase 3 contract:
// {"translated_markdown", "segments"}; source_markdown is assembled by the
// pipeline and never returned by the model).
func textPayload(source, translatedMarkdown string, segmentTranslations ...string) string {
	segments := make([]dto.Segment, 0, len(segmentTranslations))
	for _, tr := range segmentTranslations {
		segments = append(segments, dto.Segment{Source: source, Translation: tr})
	}
	raw, err := json.Marshal(map[string]any{
		"translated_markdown": translatedMarkdown,
		"segments":            segments,
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// wordPayload builds a schema-valid WordTranslation payload.
func wordPayload(word string) string {
	raw, err := json.Marshal(dto.WordTranslation{
		Word:       word,
		Lemma:      word,
		PhoneticUK: "/wɜːd/",
		PhoneticUS: "/wɝːd/",
		PartsOfSpeech: []dto.PartOfSpeech{
			{Part: "noun", Meanings: []string{"测试释义"}},
		},
		Synonyms:    []string{"sample"},
		Inflections: []string{word + "s"},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func newScriptHarness(t *testing.T, provider llm.Provider) *harness {
	t.Helper()
	h := newHarness(t)
	p, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	h.pipeline = p
	return h
}

const spanInput = "Check `config.yaml` and visit https://example.com/docs.\n\n" +
	"```go\nfmt.Println(42)\n```\n\nUse C:\\data\\cache please. Version 2.5 shipped."

func TestProtectedSpansMaskedAndRestoredVerbatim(t *testing.T) {
	masked := protectedspan.Mask(spanInput)
	if len(masked.Spans) < 4 {
		t.Fatalf("expected several protected spans, got %d: %+v", len(masked.Spans), masked.Spans)
	}
	// Script a payload that echoes the placeholders back like a compliant
	// model would.
	md := masked.Text
	provider := newScriptProvider(scriptStep{content: textPayload(md, "译文：\n\n"+md, "译文：\n\n"+md)})
	h := newScriptHarness(t, provider)

	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: spanInput, ForceKind: nlp.KindText}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	reqs := provider.requests()
	if len(reqs) == 0 {
		t.Fatal("no provider requests recorded")
	}
	if strings.Contains(reqs[0].Prompt, "https://example.com/docs") ||
		strings.Contains(reqs[0].Prompt, "fmt.Println(42)") {
		t.Errorf("provider prompt contained unprotected content: %q", reqs[0].Prompt)
	}
	if !strings.Contains(reqs[0].Prompt, masked.Spans[0].Placeholder) {
		t.Errorf("provider prompt missing placeholders: %q", reqs[0].Prompt)
	}
	// The restored result must contain every protected snippet verbatim.
	result := resp.Result.(map[string]any)
	out := result["translated_markdown"].(string)
	for _, want := range []string{"`config.yaml`", "https://example.com/docs",
		"fmt.Println(42)", `C:\data\cache`, "2.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("restored translation missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, protectedspan.PlaceholderPrefix) {
		t.Errorf("placeholder leaked into result: %q", out)
	}
	// The system prompt must carry the frozen rules.
	if !strings.Contains(reqs[0].SystemPrompt, "仅将英文翻译为简体中文") ||
		!strings.Contains(reqs[0].SystemPrompt, "术语表是硬约束") ||
		!strings.Contains(reqs[0].SystemPrompt, "占位符") {
		t.Errorf("system prompt missing frozen rules: %q", reqs[0].SystemPrompt)
	}
}

func TestPlaceholderLossTriggersRepair(t *testing.T) {
	masked := protectedspan.Mask(spanInput)
	lostMD := strings.ReplaceAll(masked.Text, masked.Spans[0].Placeholder, "")
	lostMD = strings.ReplaceAll(lostMD, masked.Spans[1].Placeholder, "")
	fixed := masked.Text

	provider := newScriptProvider(
		scriptStep{content: textPayload("src", lostMD, lostMD)},
		scriptStep{content: textPayload("src", "译文 "+fixed, "译文 "+fixed)},
	)
	h := newScriptHarness(t, provider)
	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: spanInput, ForceKind: nlp.KindText}, Options{})
	if err != nil {
		t.Fatalf("repair after placeholder loss should succeed: %v", err)
	}
	if provider.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (original + span repair)", provider.callCount())
	}
	out := resp.Result.(map[string]any)["translated_markdown"].(string)
	if !strings.Contains(out, masked.Spans[0].Content) {
		t.Errorf("repaired result missing restored content: %q", out)
	}
}

func TestPlaceholderLossAfterRepairFails(t *testing.T) {
	masked := protectedspan.Mask(spanInput)
	lostMD := strings.ReplaceAll(masked.Text, masked.Spans[0].Placeholder, "")

	provider := newScriptProvider(
		scriptStep{content: textPayload("src", lostMD, lostMD)},
		scriptStep{content: textPayload("src", lostMD, lostMD)}, // repair also loses it
	)
	h := newScriptHarness(t, provider)
	_, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: spanInput, ForceKind: nlp.KindText}, Options{})
	if err == nil {
		t.Fatal("expected TRANSLATION_FAILED after failed span repair")
	}
	e := apperr.AsE(err)
	if e.Code != apperr.CodeTranslationFailed || e.Retryable {
		t.Errorf("code = %s retryable = %v, want TRANSLATION_FAILED non-retryable", e.Code, e.Retryable)
	}
	if e.Details["reason"] != "protected_span_lost" {
		t.Errorf("details.reason = %v, want protected_span_lost", e.Details["reason"])
	}
	if provider.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (exactly one repair attempt)", provider.callCount())
	}
}

func TestTerminologySatisfiedNeedsNoRepair(t *testing.T) {
	h := newHarness(t)
	if _, err := h.terms.Create(context.Background(), "API", "接口"); err != nil {
		t.Fatalf("create term: %v", err)
	}
	provider := newScriptProvider(scriptStep{content: textPayload("src", "该接口很快。", "该接口很快。")})
	p, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if _, err := p.Translate(context.Background(),
		dto.TranslationRequest{Text: "The API is fast", ForceKind: nlp.KindText}, Options{}); err != nil {
		t.Fatalf("translate: %v", err)
	}
	if provider.callCount() != 1 {
		t.Errorf("calls = %d, want 1 (no repair)", provider.callCount())
	}
}

func TestTerminologyViolationRepaired(t *testing.T) {
	h := newHarness(t)
	if _, err := h.terms.Create(context.Background(), "API", "接口"); err != nil {
		t.Fatalf("create term: %v", err)
	}
	provider := newScriptProvider(
		scriptStep{content: textPayload("src", "The API is fast.", "The API is fast.")},
		scriptStep{content: textPayload("src", "该接口很快。", "该接口很快。")},
	)
	p, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	resp, err := p.Translate(context.Background(),
		dto.TranslationRequest{Text: "The API is fast", ForceKind: nlp.KindText}, Options{})
	if err != nil {
		t.Fatalf("terminology repair should succeed: %v", err)
	}
	if provider.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (original + terminology repair)", provider.callCount())
	}
	// The repair prompt must name the violated term explicitly.
	reqs := provider.requests()
	if !strings.Contains(reqs[1].Prompt, "API") || !strings.Contains(reqs[1].Prompt, "接口") {
		t.Errorf("repair prompt missing violated terms: %q", reqs[1].Prompt)
	}
	out := resp.Result.(map[string]any)["translated_markdown"].(string)
	if strings.Contains(out, "API") {
		t.Errorf("violating term survived: %q", out)
	}
}

func TestTerminologyViolationAfterRepairFails(t *testing.T) {
	h := newHarness(t)
	if _, err := h.terms.Create(context.Background(), "API", "接口"); err != nil {
		t.Fatalf("create term: %v", err)
	}
	provider := newScriptProvider(
		scriptStep{content: textPayload("src", "The API is fast.", "The API is fast.")},
		scriptStep{content: textPayload("src", "The API is fast.", "The API is fast.")},
	)
	p, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	_, err = p.Translate(context.Background(),
		dto.TranslationRequest{Text: "The API is fast", ForceKind: nlp.KindText}, Options{})
	if err == nil {
		t.Fatal("expected TRANSLATION_FAILED")
	}
	e := apperr.AsE(err)
	if e.Code != apperr.CodeTranslationFailed || e.Retryable {
		t.Errorf("code = %s retryable = %v", e.Code, e.Retryable)
	}
	if e.Details["reason"] != "terminology_violation" {
		t.Errorf("details.reason = %v", e.Details["reason"])
	}
	violated, ok := e.Details["violated"].([]string)
	if !ok || len(violated) == 0 || !strings.Contains(violated[0], "API") {
		t.Errorf("details.violated missing: %v", e.Details)
	}
}

// switchResolver lets tests swap the provider resolver at runtime.
type switchResolver struct {
	mu sync.Mutex
	r  llm.Resolver
}

func (s *switchResolver) set(r llm.Resolver) {
	s.mu.Lock()
	s.r = r
	s.mu.Unlock()
}

func (s *switchResolver) TranslationProvider(ctx context.Context) (llm.Provider, error) {
	s.mu.Lock()
	r := s.r
	s.mu.Unlock()
	return r.TranslationProvider(ctx)
}

func TestProviderNotConfiguredAndCacheInteraction(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sw := &switchResolver{r: llm.FixedResolver{P: h.provider}}
	p, err := NewPipeline(sw, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	// Prime the cache while configured.
	if _, err := p.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{}); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Cache hit is still served when the provider later becomes unconfigured.
	sw.set(llm.NotConfiguredResolver{})
	resp, err := p.Translate(ctx, dto.TranslationRequest{Text: "suspended"}, Options{})
	if err != nil {
		t.Fatalf("cache hit without provider should work: %v", err)
	}
	if resp.Source != SourceCache {
		t.Errorf("source = %s, want cache", resp.Source)
	}

	// Cache miss (bypass) → PROVIDER_NOT_CONFIGURED, non-retryable.
	_, err = p.Translate(ctx, dto.TranslationRequest{Text: "brand new phrase", BypassCache: true}, Options{})
	if err == nil {
		t.Fatal("expected PROVIDER_NOT_CONFIGURED")
	}
	e := apperr.AsE(err)
	if e.Code != apperr.CodeProviderNotConfigured || e.Retryable {
		t.Errorf("code = %s retryable = %v", e.Code, e.Retryable)
	}
	if h.provider.Calls() != 1 {
		t.Errorf("mock must not be consulted, calls=%d", h.provider.Calls())
	}
}

func TestCustomTranslationPromptAppendedAsPreference(t *testing.T) {
	h := newHarness(t)
	if err := h.settings.Put(context.Background(), "custom_translation_prompt", `"取正式语气"`); err != nil {
		t.Fatalf("put setting: %v", err)
	}
	provider := newScriptProvider(scriptStep{content: wordPayload("suspended")})
	p, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if _, err := p.Translate(context.Background(), dto.TranslationRequest{Text: "suspended"}, Options{}); err != nil {
		t.Fatalf("translate: %v", err)
	}
	reqs := provider.requests()
	if !strings.Contains(reqs[0].SystemPrompt, "取正式语气") {
		t.Errorf("custom prompt missing from system prompt: %q", reqs[0].SystemPrompt)
	}
	if !strings.Contains(reqs[0].SystemPrompt, "偏好") {
		t.Errorf("custom prompt must be marked preference-only: %q", reqs[0].SystemPrompt)
	}
}

func TestWordKindProtectedNumberRestored(t *testing.T) {
	// force_kind=word on an alphanumeric token: the masked input carries a
	// placeholder and the word payload must still pass the full chain.
	masked := protectedspan.Mask("COVID19")
	provider := newScriptProvider(scriptStep{content: wordPayload(masked.Text)})
	h := newScriptHarness(t, provider)
	resp, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: "COVID19", ForceKind: nlp.KindWord}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	word := resp.Result.(map[string]any)
	if word["word"] != "COVID19" {
		t.Errorf("word = %v, want COVID19 restored", word["word"])
	}
}

// --- Phase 3: long text chunking ---

// chunkEchoProvider is a Phase 3 test provider for chunked requests: it
// echoes req.Input (the chunk) back as the per-chunk payload and records
// every call in order. Selected calls can be scripted to fail, return
// invalid JSON, return a raw payload or strip a placeholder from the echo.
type chunkEchoProvider struct {
	mu      sync.Mutex
	failOn  map[int]error
	invalid map[int]bool
	raw     map[int]string
	strip   map[int]string
	calls   int
	inputs  []string
	prompts []string
}

func newChunkEchoProvider() *chunkEchoProvider {
	return &chunkEchoProvider{
		failOn: map[int]error{}, invalid: map[int]bool{},
		raw: map[int]string{}, strip: map[int]string{},
	}
}

func (c *chunkEchoProvider) Complete(_ context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.calls
	c.calls++
	c.inputs = append(c.inputs, req.Input)
	c.prompts = append(c.prompts, req.Prompt)
	if err, ok := c.failOn[i]; ok {
		return llm.CompleteResponse{}, err
	}
	if content, ok := c.raw[i]; ok {
		return llm.CompleteResponse{Content: content}, nil
	}
	input := req.Input
	if ph, ok := c.strip[i]; ok {
		input = strings.ReplaceAll(input, ph, "")
	}
	if c.invalid[i] {
		return llm.CompleteResponse{Content: `{"unexpected":true}`}, nil
	}
	payload, err := json.Marshal(map[string]any{
		"translated_markdown": "【译】" + input,
		"segments":            []dto.Segment{{Source: input, Translation: "【译】" + input}},
	})
	if err != nil {
		return llm.CompleteResponse{}, llm.ErrUnavailable
	}
	return llm.CompleteResponse{Content: string(payload)}, nil
}

func (c *chunkEchoProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsThinking: true, SupportsStructuredOutput: true}
}

func (c *chunkEchoProvider) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *chunkEchoProvider) requestInputs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.inputs...)
}

func (c *chunkEchoProvider) requestPrompts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.prompts...)
}

// threeParagraphs builds three identical ~1380-rune paragraphs: each fits the
// 1800 budget, any two together do not, so the frozen chunking yields exactly
// three chunks in order.
func threeParagraphs() string {
	para := repeatSentence(30)
	return para + "\n\n" + para + "\n\n" + para
}

func newChunkHarness(t *testing.T, provider llm.Provider) *harness {
	t.Helper()
	h := newHarness(t)
	p, err := NewPipeline(llm.FixedResolver{P: provider}, func() string { return DefaultTranslationModel },
		h.terms, h.cache, h.history, h.settings)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	h.pipeline = p
	return h
}

func countCacheRows(t *testing.T, h *harness) int {
	t.Helper()
	var n int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM translation_cache").Scan(&n); err != nil {
		t.Fatalf("count cache rows: %v", err)
	}
	return n
}

func TestChunkedTextPipelineEndToEnd(t *testing.T) {
	provider := newChunkEchoProvider()
	h := newChunkHarness(t, provider)

	input := threeParagraphs()
	// The pipeline chunks, caches and stores the NORMALIZED input (outer
	// whitespace trimmed); chunks come from that masked-normalized text.
	norm := nlp.NormalizeInput(input)
	wantChunks := SplitMarkdown(norm, DefaultChunkBudget)
	if len(wantChunks) != 3 {
		t.Fatalf("precondition: expected 3 chunks, got %d", len(wantChunks))
	}

	resp, err := h.pipeline.Translate(context.Background(), dto.TranslationRequest{Text: input}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Kind != nlp.KindText || resp.Source != SourceModel || resp.Model != DefaultTranslationModel {
		t.Errorf("kind/source/model = %s/%s/%s", resp.Kind, resp.Source, resp.Model)
	}

	// Exactly one provider call per chunk, sequential and in order.
	inputs := provider.requestInputs()
	if len(inputs) != len(wantChunks) {
		t.Fatalf("provider calls = %d, want %d", len(inputs), len(wantChunks))
	}
	for i := range wantChunks {
		if inputs[i] != wantChunks[i] {
			t.Errorf("chunk %d input mismatch:\n got %q\nwant %q", i, inputs[i], wantChunks[i])
		}
	}

	// Assembled result: the normalized full input unchanged, ordered chunk
	// translations and segments aligned to the chunks.
	result := resp.Result.(map[string]any)
	if result["source_markdown"] != norm {
		t.Errorf("source_markdown must be the full input unchanged")
	}
	wantMD := make([]string, 0, len(wantChunks))
	for _, c := range wantChunks {
		wantMD = append(wantMD, "【译】"+c)
	}
	if result["translated_markdown"] != strings.Join(wantMD, "\n\n") {
		t.Errorf("translated_markdown is not the ordered chunk translations joined with blank lines")
	}
	segs := result["segments"].([]any)
	if len(segs) != len(wantChunks) {
		t.Fatalf("segments = %d, want %d", len(segs), len(wantChunks))
	}
	for i, seg := range segs {
		sm := seg.(map[string]any)
		if sm["source"] != wantChunks[i] {
			t.Errorf("segment %d source not aligned to chunk %d", i, i)
		}
	}

	// Exactly one cache entry keyed on the FULL normalized input and one
	// history row.
	if n := countCacheRows(t, h); n != 1 {
		t.Errorf("cache rows = %d, want 1 (single entry for the full input)", n)
	}
	if _, err := h.cache.Get(context.Background(), CacheKey(norm, nlp.KindText,
		DefaultTranslationModel, ConfigHash(DefaultTranslationModel, nil))); err != nil {
		t.Errorf("cache row keyed on the full input missing: %v", err)
	}
	if n := countHistory(t, h); n != 1 {
		t.Errorf("history rows = %d, want 1", n)
	}
}

func TestChunkedInputIsProtectedAndPromptsCarryChunks(t *testing.T) {
	input := threeParagraphs() + "\n\nVisit https://example.com/guide today for more."
	provider := newChunkEchoProvider()
	h := newChunkHarness(t, provider)

	resp, err := h.pipeline.Translate(context.Background(), dto.TranslationRequest{Text: input}, Options{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	prompts := provider.requestPrompts()
	if len(prompts) == 0 {
		t.Fatal("no prompts recorded")
	}
	if strings.Contains(prompts[0], "https://example.com/guide") {
		t.Errorf("first chunk prompt must not contain later protected content: %q", prompts[0])
	}
	found := false
	for _, pr := range prompts {
		if strings.Contains(pr, protectedspan.PlaceholderPrefix) {
			found = true
		}
	}
	if !found {
		t.Errorf("no chunk prompt carried the protected placeholder")
	}
	out := resp.Result.(map[string]any)["translated_markdown"].(string)
	if !strings.Contains(out, "https://example.com/guide") {
		t.Errorf("restored translation missing the protected URL: %q", out)
	}
	if strings.Contains(out, protectedspan.PlaceholderPrefix) {
		t.Errorf("placeholder leaked into the assembled result: %q", out)
	}
}

func TestChunkProviderFailurePersistsNothing(t *testing.T) {
	provider := newChunkEchoProvider()
	provider.failOn[1] = llm.ErrConnection // second chunk fails
	h := newChunkHarness(t, provider)

	_, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: threeParagraphs()}, Options{})
	if err == nil {
		t.Fatal("any chunk failure must fail the whole request")
	}
	if c, retryable := code(t, err); c != apperr.CodeProviderConnectionFailed || !retryable {
		t.Errorf("code = %s retryable = %v, want PROVIDER_CONNECTION_FAILED retryable", c, retryable)
	}
	if provider.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (sequential: stop at the failed chunk)", provider.callCount())
	}
	if n := countCacheRows(t, h); n != 0 {
		t.Errorf("cache rows = %d, want 0 (no partial results persisted)", n)
	}
	if n := countHistory(t, h); n != 0 {
		t.Errorf("history rows = %d, want 0 (no partial results persisted)", n)
	}
}

func TestChunkInvalidPayloadFailsWithChunkIndex(t *testing.T) {
	provider := newChunkEchoProvider()
	// Second chunk returns schema-invalid JSON and so does its single
	// schema-repair attempt (provider call index 2).
	provider.invalid[1] = true
	provider.invalid[2] = true
	h := newChunkHarness(t, provider)

	_, err := h.pipeline.Translate(context.Background(),
		dto.TranslationRequest{Text: threeParagraphs()}, Options{})
	if err == nil {
		t.Fatal("expected STRUCTURED_OUTPUT_INVALID")
	}
	e := apperr.AsE(err)
	if e.Code != apperr.CodeStructuredOutputInvalid || e.Retryable {
		t.Errorf("code = %s retryable = %v, want STRUCTURED_OUTPUT_INVALID non-retryable", e.Code, e.Retryable)
	}
	if idx, ok := e.Details["chunk_index"].(int); !ok || idx != 1 {
		t.Errorf("details.chunk_index = %v, want 1", e.Details["chunk_index"])
	}
	// chunk 1 call + its single schema repair attempt (which is also invalid).
	if provider.callCount() != 3 {
		t.Errorf("calls = %d, want 3", provider.callCount())
	}
	if n := countCacheRows(t, h); n != 0 {
		t.Errorf("cache rows = %d, want 0", n)
	}
	if n := countHistory(t, h); n != 0 {
		t.Errorf("history rows = %d, want 0", n)
	}
}

func TestChunkedSpanLossRepairedAndRestored(t *testing.T) {
	input := threeParagraphs() + "\n\nVisit https://example.com/guide today for more."
	masked := protectedspan.Mask(input)
	if len(masked.Spans) != 1 {
		t.Fatalf("expected exactly 1 protected span, got %d", len(masked.Spans))
	}
	// Locate the chunk that carries the placeholder (the tail paragraph may
	// merge into the last long chunk).
	chunks := SplitMarkdown(masked.Text, DefaultChunkBudget)
	spanChunk := -1
	for i, c := range chunks {
		if strings.Contains(c, masked.Spans[0].Placeholder) {
			spanChunk = i
		}
	}
	if spanChunk < 0 {
		t.Fatal("placeholder chunk not found")
	}

	provider := newChunkEchoProvider()
	provider.strip[spanChunk] = masked.Spans[0].Placeholder // drops it initially
	h := newChunkHarness(t, provider)

	resp, err := h.pipeline.Translate(context.Background(), dto.TranslationRequest{Text: input}, Options{})
	if err != nil {
		t.Fatalf("chunk-scoped span repair should succeed: %v", err)
	}
	// One call per chunk + the single span repair call.
	if provider.callCount() != len(chunks)+1 {
		t.Errorf("calls = %d, want %d", provider.callCount(), len(chunks)+1)
	}
	out := resp.Result.(map[string]any)["translated_markdown"].(string)
	if !strings.Contains(out, "https://example.com/guide") {
		t.Errorf("repaired result missing restored URL: %q", out)
	}
}

func TestRetranslateLongTextRechunks(t *testing.T) {
	provider := newChunkEchoProvider()
	h := newChunkHarness(t, provider)
	ctx := context.Background()
	input := threeParagraphs()

	first, err := h.pipeline.Translate(ctx, dto.TranslationRequest{Text: input}, Options{})
	if err != nil {
		t.Fatalf("first translate: %v", err)
	}
	if provider.callCount() != 3 {
		t.Fatalf("first translate calls = %d, want 3", provider.callCount())
	}

	// Retranslate bypasses the cache and re-runs chunking, updating the same
	// history row in place.
	second, err := h.pipeline.Translate(ctx,
		dto.TranslationRequest{Text: input, BypassCache: true}, Options{HistoryID: first.TranslationID})
	if err != nil {
		t.Fatalf("retranslate: %v", err)
	}
	if provider.callCount() != 6 {
		t.Errorf("retranslate must re-chunk, calls = %d, want 6", provider.callCount())
	}
	if second.TranslationID != first.TranslationID {
		t.Errorf("retranslate must update the same history row")
	}
	if second.Source != SourceModel {
		t.Errorf("retranslate source = %s, want model", second.Source)
	}
	if n := countHistory(t, h); n != 1 {
		t.Errorf("history rows = %d, want 1 (updated in place)", n)
	}
	if n := countCacheRows(t, h); n != 1 {
		t.Errorf("cache rows = %d, want 1 (overwritten in place)", n)
	}
}
