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
	resolver   llm.Resolver
	model      func() string
	terms      *terminology.Service
	cache      *repository.CacheRepo
	history    *repository.HistoryRepo
	settings   *repository.SettingsRepo
	wordSchema *jsonschema.Schema
	textSchema *jsonschema.Schema
	now        func() time.Time
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
	return &Pipeline{
		resolver:   resolver,
		model:      model,
		terms:      terms,
		cache:      cache,
		history:    history,
		settings:   settings,
		wordSchema: wordSchema,
		textSchema: textSchema,
		now:        time.Now,
	}, nil
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

	// Protect spans first, then apply terminology to the masked text so the
	// replacement can never corrupt protected content (docs/06 §7). The
	// masked, terminology-applied text is what the provider sees.
	masked := protectedspan.Mask(input)
	termApplied, appliedTerms := p.terms.Apply(masked.Text)
	terms := p.terms.Snapshot()
	systemPrompt := buildSystemPrompt(kind, terms, p.customTranslationPrompt(ctx))

	raw, err := provider.Complete(ctx, llm.CompleteRequest{
		Model:        model,
		Kind:         kind,
		Input:        termApplied,
		SystemPrompt: systemPrompt,
		Prompt:       buildUserPrompt(kind, termApplied, p.schemaJSON(kind)),
		SchemaJSON:   p.schemaJSON(kind),
	})
	if err != nil {
		return p.providerFailed(ctx, err, cacheKey, opts, req, kind)
	}

	// Stage 1: schema validation with exactly one repair attempt (docs/06 §6).
	payloadMasked, lastRaw, verr := p.completeAndDecode(ctx, provider, kind, systemPrompt, termApplied, model, raw)
	if verr != nil {
		return dto.TranslationResponse{}, apperr.New(apperr.CodeStructuredOutputInvalid,
			"模型输出不符合约定结构，自动修复后仍然失败", false,
		).WithDetails(map[string]any{"validation_error": truncate(verr.Error(), maxValidationErrorLen)})
	}

	// Stage 2: restore protected spans; one repair attempt on placeholder
	// loss, then TRANSLATION_FAILED (non-retryable) with details.
	payloadRestored, lost := restoreSpans(kind, payloadMasked, masked.Spans)
	if len(lost) > 0 {
		repair, rerr2 := provider.Complete(ctx, llm.CompleteRequest{
			Model:        model,
			Kind:         kind,
			Input:        termApplied,
			SystemPrompt: systemPrompt,
			Prompt: buildSpanRepairPrompt(kind, lastRaw, missingSpans(masked.Spans, lost),
				p.schemaJSON(kind)),
			SchemaJSON: p.schemaJSON(kind),
			RepairOf:   lastRaw,
		})
		if rerr2 != nil {
			return p.providerFailed(ctx, rerr2, cacheKey, opts, req, kind)
		}
		payloadMasked2, lastRaw2, verr2 := p.completeAndDecode(ctx, provider, kind, systemPrompt, termApplied, model, repair)
		if verr2 != nil {
			return dto.TranslationResponse{}, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复失败", false,
			).WithDetails(map[string]any{
				"reason":           "protected_span_lost",
				"missing":          lost,
				"validation_error": truncate(verr2.Error(), maxValidationErrorLen),
			})
		}
		payloadRestored, lost = restoreSpans(kind, payloadMasked2, masked.Spans)
		if len(lost) > 0 {
			return dto.TranslationResponse{}, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复后仍然丢失", false,
			).WithDetails(map[string]any{"reason": "protected_span_lost", "missing": lost})
		}
		payloadMasked, lastRaw = payloadMasked2, lastRaw2
	}

	// Stage 3: terminology post-check (docs/09) on the masked payload so
	// protected content cannot trigger false violations; one repair attempt.
	violated := terminology.FindViolations(flattenStrings(payloadMasked), appliedTerms)
	if len(violated) > 0 {
		repair, rerr3 := provider.Complete(ctx, llm.CompleteRequest{
			Model:        model,
			Kind:         kind,
			Input:        termApplied,
			SystemPrompt: systemPrompt,
			Prompt:       buildTerminologyRepairPrompt(kind, lastRaw, violated, p.schemaJSON(kind)),
			SchemaJSON:   p.schemaJSON(kind),
			RepairOf:     lastRaw,
		})
		if rerr3 != nil {
			return p.providerFailed(ctx, rerr3, cacheKey, opts, req, kind)
		}
		payloadMasked3, _, verr3 := p.completeAndDecode(ctx, provider, kind, systemPrompt, termApplied, model, repair)
		if verr3 != nil {
			return dto.TranslationResponse{}, apperr.New(apperr.CodeTranslationFailed,
				"译文未遵守术语表，自动修复失败", false,
			).WithDetails(map[string]any{
				"reason":           "terminology_violation",
				"violated":         violatedTerms(violated),
				"validation_error": truncate(verr3.Error(), maxValidationErrorLen),
			})
		}
		payloadRestored, lost = restoreSpans(kind, payloadMasked3, masked.Spans)
		if len(lost) > 0 {
			return dto.TranslationResponse{}, apperr.New(apperr.CodeTranslationFailed,
				"受保护内容在译文中丢失，自动修复后仍然丢失", false,
			).WithDetails(map[string]any{"reason": "protected_span_lost", "missing": lost})
		}
		if v2 := terminology.FindViolations(flattenStrings(payloadMasked3), appliedTerms); len(v2) > 0 {
			return dto.TranslationResponse{}, apperr.New(apperr.CodeTranslationFailed,
				"译文未遵守术语表，自动修复后仍然违反", false,
			).WithDetails(map[string]any{"reason": "terminology_violation", "violated": violatedTerms(v2)})
		}
	}

	nowStr := p.now().UTC().Format(time.RFC3339)
	resultJSON, merr := json.Marshal(payloadRestored)
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
		InputText:      req.Text,
		NormalizedText: input,
		ResultJSON:     string(resultJSON),
		Source:         SourceModel,
		Model:          model,
		CreatedAt:      nowStr,
		LastViewedAt:   nowStr,
	}
	if opts.HistoryID != "" {
		err = p.history.UpdateResult(ctx, id, histRow)
	} else {
		err = p.history.Insert(ctx, histRow)
	}
	if err != nil {
		return dto.TranslationResponse{}, apperr.Wrap(apperr.CodeDatabaseError, "写入翻译历史失败", true, err)
	}

	return dto.TranslationResponse{
		TranslationID: id,
		Kind:          kind,
		Source:        SourceModel,
		Result:        payloadRestored,
		Model:         model,
		CreatedAt:     nowStr,
	}, nil
}

// completeAndDecode runs schema validation for raw and, on failure, the
// single schema-repair attempt (docs/06 §6). It returns the decoded payload
// and the raw content of the attempt that produced it.
func (p *Pipeline) completeAndDecode(ctx context.Context, provider llm.Provider, kind,
	systemPrompt, termApplied, model string, raw llm.CompleteResponse) (any, string, error) {
	payload, verr := p.validateAndDecode(kind, raw.Content)
	if verr == nil {
		return payload, raw.Content, nil
	}
	repair, rerr := provider.Complete(ctx, llm.CompleteRequest{
		Model:        model,
		Kind:         kind,
		Input:        termApplied,
		SystemPrompt: systemPrompt,
		Prompt:       buildSchemaRepairPrompt(kind, raw.Content, p.schemaJSON(kind)),
		SchemaJSON:   p.schemaJSON(kind),
		RepairOf:     raw.Content,
	})
	if rerr != nil {
		return nil, "", rerr
	}
	payload, verr = p.validateAndDecode(kind, repair.Content)
	if verr != nil {
		return nil, "", verr
	}
	return payload, repair.Content, nil
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

// providerFailed implements "API 失败但有缓存：展示缓存并标注本地缓存";
// without a cache the provider error is mapped to a retryable standard error.
func (p *Pipeline) providerFailed(ctx context.Context, err error, cacheKey string, opts Options,
	req dto.TranslationRequest, kind string) (dto.TranslationResponse, error) {
	mapped := mapProviderError(err)
	if row, cerr := p.cache.Get(ctx, cacheKey); cerr == nil {
		if resp, ferr := p.fromCache(ctx, row, opts, req.Text); ferr == nil {
			// Only the stable error code is logged — never the raw cause.
			slog.WarnContext(ctx, "provider failed, serving cached translation",
				"code", string(apperr.AsE(mapped).Code))
			return resp, nil
		}
	}
	return dto.TranslationResponse{}, mapped
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
// against the embedded schema for kind.
func (p *Pipeline) validateAndDecode(kind, raw string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("payload is not valid JSON: %w", err)
	}
	var schema *jsonschema.Schema
	switch kind {
	case nlp.KindWord:
		schema = p.wordSchema
	case nlp.KindText:
		schema = p.textSchema
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
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
