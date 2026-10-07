// Package translation implements the translation pipeline (docs/06 §7):
//
//	input → language detect → classify → normalize → protected span →
//	terminology → cache → provider → validate/repair → restore span →
//	terminology post-check → persist → response
//
// Provider responses are validated against the embedded JSON Schemas. For
// every failure class (schema, protected-span loss, terminology violation)
// exactly one repair attempt is made before failing with the standardized
// error. Since Phase 2 the provider is resolved per request: when the
// provider settings are configured the real OpenAI-compatible adapter runs;
// otherwise the request fails with PROVIDER_NOT_CONFIGURED. The mock
// provider remains a test fixture injected through llm.Resolver and is never
// reachable via HTTP.
//
// Since Phase 3 the text path is chunked (docs/06 §8): protected spans and
// terminology are applied to the FULL normalized input first, the masked
// text is then split by SplitMarkdown and the chunks are translated
// sequentially with the per-chunk structured output contract. The result is
// assembled only after every chunk succeeded (all-or-nothing: any chunk
// failure fails the whole request and nothing is persisted). The word path
// is unchanged.
package translation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/nlp"
	"github.com/Weber-5/FloatTranslate/backend/internal/protectedspan"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/terminology"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation/schemas"
	"github.com/Weber-5/FloatTranslate/backend/internal/ulcid"
)

const (
	// SourceModel marks a result produced by a provider call.
	SourceModel = "model"
	// SourceCache marks a result served from translation_cache.
	SourceCache = "cache"

	// DefaultTranslationModel is the default translation model string.
	DefaultTranslationModel = "deepseek-flash"

	// settingsKeyCustomTranslationPrompt is the settings row holding the
	// user's custom translation prompt (preference-only, docs/06 §3).
	settingsKeyCustomTranslationPrompt = "custom_translation_prompt"

	maxValidationErrorLen = 500
)

// schemasWord / schemasText return the embedded schema JSON strings.
func schemasWord() string { return schemas.WordTranslation() }
func schemasText() string { return schemas.TextTranslation() }

// compileSchema compiles one embedded schema document.
func compileSchema(raw string) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	// Use the document's own $id as the resource URL when present.
	url := "inline.json"
	if m, ok := doc.(map[string]any); ok {
		if id, ok := m["$id"].(string); ok && id != "" {
			url = id
		}
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(url, doc); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
}

// Pipeline wires the translation flow together.
type Pipeline struct {
	resolver    llm.Resolver
	model       func() string
	terms       *terminology.Service
	cache       *repository.CacheRepo
	history     *repository.HistoryRepo
	settings    *repository.SettingsRepo
	wordSchema  *jsonschema.Schema
	textSchema  *jsonschema.Schema
	chunkSchema *jsonschema.Schema
	now         func() time.Time
}

// Options tweaks a single Translate call.
type Options struct {
	// HistoryID, when non-empty, updates that history row in place instead
	// of inserting a new one (retranslate flow: the current history entry's
	// result is refreshed). The response reuses this ID.
	HistoryID string
}

// NewPipeline builds a pipeline, compiling the embedded schemas once.
// resolver supplies the provider per request (the real adapter in production
// wiring, the mock as a test fixture). settings (optional) is read for the
// custom translation prompt.
func NewPipeline(resolver llm.Resolver, model func() string, terms *terminology.Service,
	cache *repository.CacheRepo, history *repository.HistoryRepo,
	settings *repository.SettingsRepo) (*Pipeline, error) {
	wordSchema, err := compileSchema(schemasWord())
	if err != nil {
		return nil, fmt.Errorf("compile word schema: %w", err)
	}
	textSchema, err := compileSchema(schemasText())
	if err != nil {
		return nil, fmt.Errorf("compile text schema: %w", err)
	}
	chunkSchema, err := compileSchema(schemas.TextChunk())
	if err != nil {
		return nil, fmt.Errorf("compile text chunk schema: %w", err)
	}
	return &Pipeline{
		resolver:    resolver,
		model:       model,
		terms:       terms,
		cache:       cache,
		history:     history,
		settings:    settings,
		wordSchema:  wordSchema,
		textSchema:  textSchema,
		chunkSchema: chunkSchema,
		now:         time.Now,
	}, nil
}

// runEnv carries the per-request environment shared by the word and text
// (chunked) translation paths.
type runEnv struct {
	provider     llm.Provider
	model        string
	systemPrompt string
	// input is the full normalized input; source_markdown of assembled text
	// results is this value unchanged.
	input string
	// termApplied is the protected-span-masked, terminology-applied full
	// text — the word path's single provider input and the text path's
	// chunking source.
	termApplied  string
	spans        []protectedspan.Span
	appliedTerms []repository.TerminologyRow
	cacheKey     string
	opts         Options
	req          dto.TranslationRequest
}

// Translate runs the full pipeline for one request.
func (p *Pipeline) Translate(ctx context.Context, req dto.TranslationRequest, opts Options) (dto.TranslationResponse, error) {
	input := nlp.NormalizeInput(req.Text)
	if input == "" {
		return dto.TranslationResponse{}, apperr.New(apperr.CodeInvalidRequest, "翻译内容不能为空", false)
	}
	if !nlp.IsSupportedLanguage(input) {
		return dto.TranslationResponse{}, apperr.New(apperr.CodeUnsupportedLanguage, "FloatTranslate 1.0 暂仅支持英译中", false)
	}
	kind := req.ForceKind
	if kind == "" {
		kind = nlp.ClassifyKind(input)
	}
	if kind != nlp.KindWord && kind != nlp.KindText {
		return dto.TranslationResponse{}, apperr.New(apperr.CodeInvalidRequest, "force_kind 仅支持 word 或 text", false)
	}

	model := p.modelFunc()
	configHash := ConfigHash(model, p.terms.Snapshot())
	cacheKey := CacheKey(input, kind, model, configHash)

	// Cache lookup is skipped when bypassing (retranslate); the fallback
	// below may still serve the cache when the provider then fails.
	if !req.BypassCache {
		row, err := p.cache.Get(ctx, cacheKey)
		switch {
		case err == nil:
			if resp, cerr := p.fromCache(ctx, row, opts, req.Text); cerr == nil {
				_ = p.cache.Touch(ctx, cacheKey, p.now().UTC().Format(time.RFC3339))
				return resp, nil
			}
			// Corrupted cache row: fall through to the provider.
		case errors.Is(err, repository.ErrNotFound):
			// Miss: continue with the provider.
		default:
			return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "读取翻译缓存失败", true, err)
		}
	}

	// Resolve the provider per request (Phase 2). A missing configuration is
	// not a provider call failure: it must NOT fall back to the cache below.
	provider, rerr := p.resolver.TranslationProvider(ctx)
	if rerr != nil {
		return dto.TranslationResponse{}, mapProviderError(rerr)
	}

	// Protect spans first, then apply terminology to the masked text. Both
	// run on the FULL input before chunking (Phase 3 frozen ordering); the
	// masked, terminology-applied text is what gets chunked.
	masked := protectedspan.Mask(input)
	termApplied, appliedTerms := p.terms.Apply(masked.Text)
	terms := p.terms.Snapshot()
	systemPrompt := buildSystemPrompt(kind, terms, p.customTranslationPrompt(ctx))

	e := &runEnv{
		provider:     provider,
		model:        model,
		systemPrompt: systemPrompt,
		input:        input,
		termApplied:  termApplied,
		spans:        masked.Spans,
		appliedTerms: appliedTerms,
		cacheKey:     cacheKey,
		opts:         opts,
		req:          req,
	}

	var payload any
	var err error
	if kind == nlp.KindWord {
		payload, err = p.translateWord(ctx, e)
	} else {
		payload, err = p.translateText(ctx, e)
	}
	if err != nil {
		var fb *cacheFallbackError
		if errors.As(err, &fb) {
			return fb.resp, nil
		}
		return dto.TranslationResponse{}, err
	}
	return p.persistAndRespond(ctx, kind, input, req.Text, opts, cacheKey, configHash, model, payload)
}

// translateWord runs the unchanged single-shot word path: one provider call,
// schema validation with one repair attempt, protected-span restore with one
// repair attempt and the terminology post-check with one repair attempt.
func (p *Pipeline) translateWord(ctx context.Context, e *runEnv) (any, error) {
	raw, err := e.provider.Complete(ctx, llm.CompleteRequest{
		Model:        e.model,
		Kind:         nlp.KindWord,
		Input:        e.termApplied,
		SystemPrompt: e.systemPrompt,
		Prompt:       buildUserPrompt(nlp.KindWord, e.termApplied),
		SchemaJSON:   p.schemaJSON(nlp.KindWord),
	})
	if err != nil {
		return nil, p.providerFailed(ctx, err, e)
	}

	// Stage 1: schema validation with exactly one repair attempt (docs/06 §6).
	payloadMasked, lastRaw, verr := p.completeAndDecode(ctx, e, nlp.KindWord, e.systemPrompt,
		e.termApplied, p.schemaJSON(nlp.KindWord), p.wordSchema, raw)
	if verr != nil {
		return nil, apperr.New(apperr.CodeStructuredOutputInvalid,
			"模型输出不符合约定结构，自动修复后仍然失败", false,
		).WithDetails(map[string]any{"validation_error": truncate(verr.Error(), maxValidationErrorLen)})
	}

	// Stage 2: restore protected spans; one repair attempt on placeholder
	// loss, then TRANSLATION_FAILED (non-retryable) with details.
	payloadRestored, lost := restoreSpans(nlp.KindWord, payloadMasked, e.spans)
	if len(lost) > 0 {
		repair, rerr2 := e.provider.Complete(ctx, llm.CompleteRequest{
			Model:        e.model,
			Kind:         nlp.KindWord,
			Input:        e.termApplied,
			SystemPrompt: e.systemPrompt,
			Prompt: buildSpanRepairPrompt(nlp.KindWord, lastRaw, missingSpans(e.spans, lost),
				p.schemaJSON(nlp.KindWord)),
			SchemaJSON: p.schemaJSON(nlp.KindWord),
			RepairOf:   lastRaw,
		})
		if rerr2 != nil {
			return nil, p.providerFailed(ctx, rerr2, e)
		}
		payloadMasked2, lastRaw2, verr2 := p.completeAndDecode(ctx, e, nlp.KindWord, e.systemPrompt,
			e.termApplied, p.schemaJSON(nlp.KindWord), p.wordSchema, repair)
		if verr2 != nil {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复失败", false,
			).WithDetails(map[string]any{
				"reason":           "protected_span_lost",
				"missing":          lost,
				"validation_error": truncate(verr2.Error(), maxValidationErrorLen),
			})
		}
		payloadRestored, lost = restoreSpans(nlp.KindWord, payloadMasked2, e.spans)
		if len(lost) > 0 {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复后仍然丢失", false,
			).WithDetails(map[string]any{"reason": "protected_span_lost", "missing": lost})
		}
		payloadMasked, lastRaw = payloadMasked2, lastRaw2
	}

	// Stage 3: terminology post-check (docs/09) on the masked payload so
	// protected content cannot trigger false violations; one repair attempt.
	if violated := terminology.FindViolations(flattenStrings(payloadMasked), e.appliedTerms); len(violated) > 0 {
		repair, rerr3 := e.provider.Complete(ctx, llm.CompleteRequest{
			Model:        e.model,
			Kind:         nlp.KindWord,
			Input:        e.termApplied,
			SystemPrompt: e.systemPrompt,
			Prompt:       buildTerminologyRepairPrompt(nlp.KindWord, lastRaw, violated, p.schemaJSON(nlp.KindWord)),
			SchemaJSON:   p.schemaJSON(nlp.KindWord),
			RepairOf:     lastRaw,
		})
		if rerr3 != nil {
			return nil, p.providerFailed(ctx, rerr3, e)
		}
		payloadMasked3, _, verr3 := p.completeAndDecode(ctx, e, nlp.KindWord, e.systemPrompt,
			e.termApplied, p.schemaJSON(nlp.KindWord), p.wordSchema, repair)
		if verr3 != nil {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"译文未遵守术语表，自动修复失败", false,
			).WithDetails(map[string]any{
				"reason":           "terminology_violation",
				"violated":         violatedTerms(violated),
				"validation_error": truncate(verr3.Error(), maxValidationErrorLen),
			})
		}
		payloadRestored, lost = restoreSpans(nlp.KindWord, payloadMasked3, e.spans)
		if len(lost) > 0 {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复后仍然丢失", false,
			).WithDetails(map[string]any{"reason": "protected_span_lost", "missing": lost})
		}
		if v2 := terminology.FindViolations(flattenStrings(payloadMasked3), e.appliedTerms); len(v2) > 0 {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"译文未遵守术语表，自动修复后仍然违反", false,
			).WithDetails(map[string]any{"reason": "terminology_violation", "violated": violatedTerms(v2)})
		}
	}

	return payloadRestored, nil
}

// translateText implements the Phase 3 chunked text path (docs/06 §8): the
// masked, terminology-applied text is split into chunks that are translated
// SEQUENTIALLY with the per-chunk structured output contract. Each chunk
// runs the same validate → span-restore → terminology post-check chain with
// exactly one repair attempt per failure class; a chunk failure fails the
// whole request (details carry chunk_index) and nothing is persisted. After
// the last chunk succeeds the result is assembled: translated_markdown is
// the chunk translations joined with "\n\n", segments are concatenated in
// chunk order and source_markdown is the original full input unchanged.
func (p *Pipeline) translateText(ctx context.Context, e *runEnv) (any, error) {
	chunks := SplitMarkdown(e.termApplied, DefaultChunkBudget)
	if len(chunks) == 0 {
		return nil, apperr.New(apperr.CodeTranslationFailed, "长文本分块失败", false)
	}

	chunkSystem := buildChunkSystemPrompt(e.systemPrompt)
	chunkSchemaJSON := schemas.TextChunk()

	mdParts := make([]string, 0, len(chunks))
	var segments []dto.Segment
	for i, chunk := range chunks {
		payload, err := p.translateChunk(ctx, e, chunkSystem, chunk, chunkSchemaJSON, i)
		if err != nil {
			var appErr *apperr.E
			if errors.As(err, &appErr) {
				return nil, err
			}
			// Provider-class failure: fall back to the cache when available
			// ("API 失败但有缓存").
			return nil, p.providerFailed(ctx, err, e)
		}
		m := payload.(map[string]any)
		translated, _ := m["translated_markdown"].(string)
		mdParts = append(mdParts, translated)
		// The provider only returns the translation now (1.1.1): the parallel
		// view is derived here by pairing the chunk's blank-line blocks with
		// the translation's. A count mismatch simply yields no segments for
		// that chunk — the translation itself is never affected.
		segments = append(segments, pairSegments(chunk, translated)...)
	}

	// Assembled TextTranslation in its decoded-JSON shape (same convention as
	// the word path and the cache round-trip): source_markdown is the
	// original full input unchanged, translated_markdown joins the chunk
	// translations with blank lines, segments are concatenated in chunk order.
	segmentsAny := make([]any, 0, len(segments))
	for _, s := range segments {
		segmentsAny = append(segmentsAny, map[string]any{
			"source":      s.Source,
			"translation": s.Translation,
		})
	}
	return map[string]any{
		"source_markdown":     e.input,
		"translated_markdown": strings.Join(mdParts, "\n\n"),
		"segments":            segmentsAny,
	}, nil
}

// pairSegments builds the 中英对照 view locally: the source and its translation
// are split into blank-line blocks and paired by position (1.1.1 — the provider
// used to emit both, which doubled the completion tokens for no extra
// information). Blocks only pair when the counts match: a mismatch means the
// model merged or split paragraphs, and guessing would mis-align the view, so
// the translation is shown without per-segment pairs instead.
func pairSegments(source, translation string) []dto.Segment {
	src := splitParagraphs(source)
	out := splitParagraphs(translation)
	if len(src) == 0 || len(src) != len(out) {
		return nil
	}
	segments := make([]dto.Segment, 0, len(src))
	for i := range src {
		segments = append(segments, dto.Segment{Source: src[i], Translation: out[i]})
	}
	return segments
}

// splitParagraphs splits markdown into its blank-line separated blocks, dropping
// empty ones and normalising CRLF. (chunker.go's splitBlocks works on a
// different block type for chunk budgeting — this one stays pure text.)
func splitParagraphs(text string) []string {
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	blocks := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			blocks = append(blocks, trimmed)
		}
	}
	return blocks
}

// translateChunk translates one chunk: provider call, per-chunk schema
// validation with one repair attempt, chunk-scoped protected-span restore
// with one repair attempt and the terminology post-check with one repair
// attempt. Provider-class errors are returned unwrapped so the caller can
// apply the cache fallback; pipeline failures carry chunk_index in details.
func (p *Pipeline) translateChunk(ctx context.Context, e *runEnv, systemPrompt, chunk, schemaJSON string, chunkIndex int) (any, error) {
	raw, err := e.provider.Complete(ctx, llm.CompleteRequest{
		Model:        e.model,
		Kind:         nlp.KindText,
		Input:        chunk,
		SystemPrompt: systemPrompt,
		Prompt:       buildUserPrompt(nlp.KindText, chunk),
		SchemaJSON:   schemaJSON,
	})
	if err != nil {
		return nil, err
	}

	payload, lastRaw, verr := p.completeAndDecode(ctx, e, nlp.KindText, systemPrompt,
		chunk, schemaJSON, p.chunkSchema, raw)
	if verr != nil {
		return nil, apperr.New(apperr.CodeStructuredOutputInvalid,
			"模型输出不符合约定结构，自动修复后仍然失败", false,
		).WithDetails(map[string]any{
			"chunk_index":      chunkIndex,
			"validation_error": truncate(verr.Error(), maxValidationErrorLen),
		})
	}

	// Protected spans: only the placeholders that occur in THIS chunk must
	// survive in this chunk's output; one repair attempt.
	relevant := spansInChunk(chunk, e.spans)
	payloadRestored, lost := restoreChunkSpans(payload, relevant)
	if len(lost) > 0 {
		repair, rerr2 := e.provider.Complete(ctx, llm.CompleteRequest{
			Model:        e.model,
			Kind:         nlp.KindText,
			Input:        chunk,
			SystemPrompt: systemPrompt,
			Prompt: buildSpanRepairPrompt(nlp.KindText, lastRaw, missingSpans(relevant, lost),
				schemaJSON),
			SchemaJSON: schemaJSON,
			RepairOf:   lastRaw,
		})
		if rerr2 != nil {
			return nil, rerr2
		}
		payload2, lastRaw2, verr2 := p.completeAndDecode(ctx, e, nlp.KindText, systemPrompt,
			chunk, schemaJSON, p.chunkSchema, repair)
		if verr2 != nil {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复失败", false,
			).WithDetails(map[string]any{
				"chunk_index":      chunkIndex,
				"reason":           "protected_span_lost",
				"missing":          lost,
				"validation_error": truncate(verr2.Error(), maxValidationErrorLen),
			})
		}
		payloadRestored, lost = restoreChunkSpans(payload2, relevant)
		if len(lost) > 0 {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复后仍然丢失", false,
			).WithDetails(map[string]any{
				"chunk_index": chunkIndex,
				"reason":      "protected_span_lost",
				"missing":     lost,
			})
		}
		payload, lastRaw = payload2, lastRaw2
	}

	// Terminology post-check per chunk; one repair attempt.
	if violated := terminology.FindViolations(flattenStrings(payload), e.appliedTerms); len(violated) > 0 {
		repair, rerr3 := e.provider.Complete(ctx, llm.CompleteRequest{
			Model:        e.model,
			Kind:         nlp.KindText,
			Input:        chunk,
			SystemPrompt: systemPrompt,
			Prompt:       buildTerminologyRepairPrompt(nlp.KindText, lastRaw, violated, schemaJSON),
			SchemaJSON:   schemaJSON,
			RepairOf:     lastRaw,
		})
		if rerr3 != nil {
			return nil, rerr3
		}
		payload3, _, verr3 := p.completeAndDecode(ctx, e, nlp.KindText, systemPrompt,
			chunk, schemaJSON, p.chunkSchema, repair)
		if verr3 != nil {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"译文未遵守术语表，自动修复失败", false,
			).WithDetails(map[string]any{
				"chunk_index":      chunkIndex,
				"reason":           "terminology_violation",
				"violated":         violatedTerms(violated),
				"validation_error": truncate(verr3.Error(), maxValidationErrorLen),
			})
		}
		payloadRestored, lost = restoreChunkSpans(payload3, relevant)
		if len(lost) > 0 {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复后仍然丢失", false,
			).WithDetails(map[string]any{
				"chunk_index": chunkIndex,
				"reason":      "protected_span_lost",
				"missing":     lost,
			})
		}
		if v2 := terminology.FindViolations(flattenStrings(payload3), e.appliedTerms); len(v2) > 0 {
			return nil, apperr.New(apperr.CodeTranslationFailed,
				"译文未遵守术语表，自动修复后仍然违反", false,
			).WithDetails(map[string]any{
				"chunk_index": chunkIndex,
				"reason":      "terminology_violation",
				"violated":    violatedTerms(v2),
			})
		}
		payloadRestored = payload3
	}

	return payloadRestored, nil
}

// persistAndRespond marshals the final payload, writes the cache row and the
// history row and builds the response. It runs only after a fully successful
// translation (all-or-nothing: no partial results are ever persisted).
func (p *Pipeline) persistAndRespond(ctx context.Context, kind, input, originalText string, opts Options,
	cacheKey, configHash, model string, payload any) (dto.TranslationResponse, error) {
	nowStr := p.now().UTC().Format(time.RFC3339)
	resultJSON, merr := json.Marshal(payload)
	if merr != nil {
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeTranslationFailed, "翻译结果序列化失败", false, merr)
	}

	if err := p.cache.Upsert(ctx, repository.CacheRow{
		CacheKey:        cacheKey,
		Kind:            kind,
		NormalizedInput: input,
		Model:           model,
		ConfigHash:      configHash,
		ResultJSON:      string(resultJSON),
		CreatedAt:       nowStr,
		UpdatedAt:       nowStr,
		LastUsedAt:      nowStr,
	}); err != nil {
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "写入翻译缓存失败", true, err)
	}

	id := opts.HistoryID
	if id == "" {
		id = ulcid.New()
	}
	histRow := repository.HistoryRow{
		ID:             id,
		Kind:           kind,
		InputText:      originalText,
		NormalizedText: input,
		ResultJSON:     string(resultJSON),
		Source:         SourceModel,
		Model:          model,
		CreatedAt:      nowStr,
		LastViewedAt:   nowStr,
	}
	if opts.HistoryID != "" {
		err := p.history.UpdateResult(ctx, id, histRow)
		if err == nil {
			return p.newResponse(id, kind, SourceModel, payload, model, nowStr), nil
		}
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "写入翻译历史失败", true, err)
	}
	if err := p.history.Insert(ctx, histRow); err != nil {
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "写入翻译历史失败", true, err)
	}
	return p.newResponse(id, kind, SourceModel, payload, model, nowStr), nil
}

func (p *Pipeline) newResponse(id, kind, source string, result any, model, createdAt string) dto.TranslationResponse {
	return dto.TranslationResponse{
		TranslationID: id,
		Kind:          kind,
		Source:        source,
		Result:        result,
		Model:         model,
		CreatedAt:     createdAt,
	}
}

// completeAndDecode runs schema validation for raw and, on failure, the
// single schema-repair attempt (docs/06 §6). It returns the decoded payload
// and the raw content of the attempt that produced it.
func (p *Pipeline) completeAndDecode(ctx context.Context, e *runEnv, kind,
	systemPrompt, input, schemaJSON string, schema *jsonschema.Schema,
	raw llm.CompleteResponse) (any, string, error) {
	payload, verr := validateAndDecode(schema, raw.Content)
	if verr == nil {
		return payload, raw.Content, nil
	}
	// Diagnostic (improvement bug #1): a repair means TWO provider calls for a
	// single translation, doubling the latency the user perceives. Only counts
	// and the failing schema location are logged — never the model output or
	// the offending values.
	slog.WarnContext(ctx, "translation output failed schema validation; issuing one repair request",
		slog.String("kind", kind), slog.Int("raw_chars", len([]rune(raw.Content))),
		slog.String("reason", schemaFailureReason(verr)))
	repair, rerr := e.provider.Complete(ctx, llm.CompleteRequest{
		Model:        e.model,
		Kind:         kind,
		Input:        input,
		SystemPrompt: systemPrompt,
		Prompt:       buildSchemaRepairPrompt(kind, raw.Content, schemaJSON),
		SchemaJSON:   schemaJSON,
		RepairOf:     raw.Content,
	})
	if rerr != nil {
		return nil, "", rerr
	}
	payload, verr = validateAndDecode(schema, repair.Content)
	if verr != nil {
		slog.WarnContext(ctx, "repair request also failed schema validation",
			slog.String("kind", kind), slog.String("reason", schemaFailureReason(verr)))
		return nil, "", verr
	}
	return payload, repair.Content, nil
}

// schemaFailureReason renders a JSON-Schema validation error as a content-free
// diagnostic: the failing keyword path plus the instance location. The
// offending value is never included (it can echo model/user content).
func schemaFailureReason(err error) string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) || ve == nil {
		return ""
	}
	// The root error often carries only the aggregate keyword ("anyOf"/
	// "oneOf"); the actionable location lives in the deepest cause.
	leaf := ve
	for len(leaf.Causes) > 0 && leaf.Causes[0] != nil {
		leaf = leaf.Causes[0]
	}
	keyword := keywordPath(leaf)
	if keyword == "" {
		keyword = keywordPath(ve)
	}
	if keyword == "" {
		keyword = "?"
	}
	return fmt.Sprintf("%s at /%s", keyword, strings.Join(leaf.InstanceLocation, "/"))
}

// keywordPath renders an error kind's keyword path ("" when unavailable).
func keywordPath(ve *jsonschema.ValidationError) string {
	if ve == nil || ve.ErrorKind == nil {
		return ""
	}
	return strings.Join(ve.ErrorKind.KeywordPath(), "/")
}

// fromCache builds a cache-sourced response and records it in history.
func (p *Pipeline) fromCache(ctx context.Context, row repository.CacheRow, opts Options, originalText string) (dto.TranslationResponse, error) {
	var result any
	if err := json.Unmarshal([]byte(row.ResultJSON), &result); err != nil {
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "缓存结果损坏", false, err)
	}
	id := opts.HistoryID
	if id == "" {
		id = ulcid.New()
	}
	histRow := repository.HistoryRow{
		ID:           id,
		Kind:         row.Kind,
		InputText:    originalText,
		ResultJSON:   row.ResultJSON,
		Source:       SourceCache,
		Model:        row.Model,
		CreatedAt:    row.UpdatedAt,
		LastViewedAt: p.now().UTC().Format(time.RFC3339),
	}
	if opts.HistoryID != "" {
		if err := p.history.UpdateResult(ctx, id, histRow); err != nil {
			return dto.TranslationResponse{}, err
		}
	} else if err := p.history.Insert(ctx, histRow); err != nil {
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "写入翻译历史失败", true, err)
	}
	return dto.TranslationResponse{
		TranslationID: id,
		Kind:          row.Kind,
		Source:        SourceCache,
		Result:        result,
		Model:         row.Model,
		CreatedAt:     row.UpdatedAt,
	}, nil
}

// cacheFallbackError carries a fully built cache-sourced response through the
// (payload, error) translation paths. When Translate sees it, the response is
// returned as-is ("API 失败但有缓存：展示缓存并标注本地缓存"): the fromCache
// helper has already recorded the history row, so nothing is persisted again.
type cacheFallbackError struct {
	resp dto.TranslationResponse
}

func (e *cacheFallbackError) Error() string { return "provider failed, serving cached translation" }

// providerFailed maps a provider error and, when a cache row exists, wraps the
// cached response in a cacheFallbackError; without a cache the provider error
// is mapped to a retryable standard error.
func (p *Pipeline) providerFailed(ctx context.Context, err error, e *runEnv) error {
	mapped := mapProviderError(err)
	if row, cerr := p.cache.Get(ctx, e.cacheKey); cerr == nil {
		if resp, ferr := p.fromCache(ctx, row, e.opts, e.req.Text); ferr == nil {
			// Only the stable error code is logged — never the raw cause.
			slog.WarnContext(ctx, "provider failed, serving cached translation",
				"code", string(apperr.AsE(mapped).Code))
			return &cacheFallbackError{resp: resp}
		}
	}
	return mapped
}

func mapProviderError(err error) error {
	switch {
	case errors.Is(err, llm.ErrNotConfigured):
		return apperr.New(apperr.CodeProviderNotConfigured, "尚未配置翻译服务，请先在设置中完成 Provider 配置", false)
	case errors.Is(err, llm.ErrConnection):
		return apperr.Wrap(apperr.CodeProviderConnectionFailed, "无法连接翻译服务，请检查网络或 Base URL 后重试", true, err)
	default:
		return apperr.Wrap(apperr.CodeProviderUnavailable, "翻译服务暂时不可用，请稍后重试", true, err)
	}
}

// validateAndDecode decodes the raw provider payload and validates it
// against the given embedded schema.
func validateAndDecode(schema *jsonschema.Schema, raw string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("payload is not valid JSON: %w", err)
	}
	if err := schema.Validate(v); err != nil {
		return nil, err
	}
	return v, nil
}

func (p *Pipeline) schemaJSON(kind string) string {
	if kind == nlp.KindWord {
		return schemasWord()
	}
	return schemasText()
}

func (p *Pipeline) modelFunc() string {
	if p.model == nil {
		return DefaultTranslationModel
	}
	if m := strings.TrimSpace(p.model()); m != "" {
		return m
	}
	return DefaultTranslationModel
}

// customTranslationPrompt reads the user's custom translation prompt from
// the settings kv store (empty = no preference appended).
func (p *Pipeline) customTranslationPrompt(ctx context.Context) string {
	if p.settings == nil {
		return ""
	}
	raw, err := p.settings.Get(ctx, settingsKeyCustomTranslationPrompt)
	if err != nil || raw == "" {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return ""
	}
	return s
}

// restoreSpans substitutes placeholders in every string of the payload and
// reports placeholders missing from the translation surface (the output as a
// whole). Coverage is evaluated on the pre-restore payload.
func restoreSpans(kind string, payload any, spans []protectedspan.Span) (any, []string) {
	if len(spans) == 0 {
		return payload, nil
	}
	var lost []string
	if kind == nlp.KindText {
		surface := translationSurface(payload)
		for _, sp := range spans {
			if !strings.Contains(strings.ToLower(surface), strings.ToLower(sp.Placeholder)) {
				lost = append(lost, sp.Placeholder)
			}
		}
	} else {
		// Word results rarely carry spans; a placeholder counts as covered
		// when it appears anywhere in the payload.
		joined := strings.ToLower(strings.Join(collectStrings(payload), "\n"))
		for _, sp := range spans {
			if !strings.Contains(joined, strings.ToLower(sp.Placeholder)) {
				lost = append(lost, sp.Placeholder)
			}
		}
	}
	return restoreDeep(payload, spans), lost
}

// spansInChunk returns the spans whose placeholders occur in chunk. Only
// these must survive this chunk's translation (chunk-scoped coverage).
func spansInChunk(chunk string, spans []protectedspan.Span) []protectedspan.Span {
	if len(spans) == 0 {
		return nil
	}
	lower := strings.ToLower(chunk)
	var out []protectedspan.Span
	for _, sp := range spans {
		if strings.Contains(lower, strings.ToLower(sp.Placeholder)) {
			out = append(out, sp)
		}
	}
	return out
}

// restoreChunkSpans restores placeholders in a chunk payload and reports the
// chunk's placeholders missing from the translation surface. Coverage is
// evaluated on the pre-restore payload.
func restoreChunkSpans(payload any, relevant []protectedspan.Span) (any, []string) {
	if len(relevant) == 0 {
		return payload, nil
	}
	surface := strings.ToLower(translationSurface(payload))
	var lost []string
	for _, sp := range relevant {
		if !strings.Contains(surface, strings.ToLower(sp.Placeholder)) {
			lost = append(lost, sp.Placeholder)
		}
	}
	return restoreDeep(payload, relevant), lost
}

// translationSurface joins the translation-bearing fields of a text payload:
// translated_markdown plus every segment translation.
func translationSurface(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(m["translated_markdown"].(string))
	if segments, ok := m["segments"].([]any); ok {
		for _, seg := range segments {
			if s, ok := seg.(map[string]any); ok {
				if tr, ok := s["translation"].(string); ok {
					sb.WriteString("\n")
					sb.WriteString(tr)
				}
			}
		}
	}
	return sb.String()
}

// restoreDeep walks the decoded JSON and replaces placeholders inside every
// string (source echoes included).
func restoreDeep(v any, spans []protectedspan.Span) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = restoreDeep(val, spans)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = restoreDeep(val, spans)
		}
		return out
	case string:
		return protectedspan.RestoreString(t, spans)
	default:
		return v
	}
}

// collectStrings gathers every string leaf of the decoded JSON.
func collectStrings(v any) []string {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for _, val := range t {
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		case string:
			out = append(out, t)
		}
	}
	walk(v)
	return out
}

// flattenStrings joins all string leaves for whole-payload scans.
func flattenStrings(v any) string {
	return strings.Join(collectStrings(v), "\n")
}

// missingSpans returns the spans whose placeholders are in lost.
func missingSpans(spans []protectedspan.Span, lost []string) []protectedspan.Span {
	var out []protectedspan.Span
	for _, sp := range spans {
		for _, ph := range lost {
			if sp.Placeholder == ph {
				out = append(out, sp)
				break
			}
		}
	}
	return out
}

// violatedTerms renders violations for error details.
func violatedTerms(rows []repository.TerminologyRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Source+" → "+r.Target)
	}
	return out
}

// CacheKey derives the translation cache key:
// sha256 hex of (normalized_input + "\x1f" + kind + "\x1f" + model + "\x1f" + config_hash).
func CacheKey(normalizedInput, kind, model, configHash string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{normalizedInput, kind, model, configHash}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// ConfigHash derives the effective translation config hash:
// sha256 hex of (translation_model + terminology snapshot).
func ConfigHash(model string, terms []repository.TerminologyRow) string {
	parts := make([]string, 0, len(terms)+1)
	parts = append(parts, model)
	for _, t := range terms {
		parts = append(parts, t.Source+"="+t.Target)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1e")))
	return hex.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
