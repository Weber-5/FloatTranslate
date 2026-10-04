// Package translation implements the Phase 1 translation pipeline
// (docs/06 §7):
//
//	input → language detect → classify → normalize → terminology →
//	cache → provider → validate/repair → persist → response
//
// Provider responses are validated against the embedded JSON Schemas; one
// repair attempt is made for invalid payloads before failing with
// STRUCTURED_OUTPUT_INVALID.
package translation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/nlp"
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

// systemPrompt encodes the frozen translation prompt rules (docs/06 §3).
const systemPrompt = `你是 FloatTranslate 的英译中翻译引擎，严格遵守以下规则：
1. 仅将英文翻译为简体中文。
2. 最终输出必须是符合给定 JSON Schema 的单个 JSON 对象；不要输出 Markdown 代码块、解释或任何前后缀。
3. 术语表是硬约束：术语表中的词条必须使用指定中文译文。
4. URL、代码、LaTeX、路径、数字、技术标识属于受保护内容，必须原样保留，不得翻译或改写。
5. 翻译永远关闭思考模式。`

// Pipeline wires the translation flow together.
type Pipeline struct {
	provider   llm.Provider
	model      func() string
	terms      *terminology.Service
	cache      *repository.CacheRepo
	history    *repository.HistoryRepo
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
func NewPipeline(provider llm.Provider, model func() string, terms *terminology.Service,
	cache *repository.CacheRepo, history *repository.HistoryRepo) (*Pipeline, error) {
	wordSchema, err := compileSchema(schemasWord())
	if err != nil {
		return nil, fmt.Errorf("compile word schema: %w", err)
	}
	textSchema, err := compileSchema(schemasText())
	if err != nil {
		return nil, fmt.Errorf("compile text schema: %w", err)
	}
	return &Pipeline{
		provider:   provider,
		model:      model,
		terms:      terms,
		cache:      cache,
		history:    history,
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

	termApplied, _ := p.terms.Apply(input)
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

	raw, err := p.provider.Complete(ctx, llm.CompleteRequest{
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

	result, verr := p.validateAndDecode(kind, raw.Content)
	if verr != nil {
		// One repair attempt, then a standardized error (docs/06 §6).
		repair, rerr := p.provider.Complete(ctx, llm.CompleteRequest{
			Model:        model,
			Kind:         kind,
			Input:        termApplied,
			SystemPrompt: systemPrompt,
			Prompt:       buildRepairPrompt(kind, raw.Content, p.schemaJSON(kind)),
			SchemaJSON:   p.schemaJSON(kind),
			RepairOf:     raw.Content,
		})
		if rerr != nil {
			return p.providerFailed(ctx, rerr, cacheKey, opts, req, kind)
		}
		result, verr = p.validateAndDecode(kind, repair.Content)
		if verr != nil {
			return dto.TranslationResponse{}, apperr.New(apperr.CodeStructuredOutputInvalid,
				"模型输出不符合约定结构，自动修复后仍然失败", false,
			).WithDetails(map[string]any{"validation_error": truncate(verr.Error(), maxValidationErrorLen)})
		}
	}

	nowStr := p.now().UTC().Format(time.RFC3339)
	resultJSON, merr := json.Marshal(result)
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
		Result:        result,
		Model:         model,
		CreatedAt:     nowStr,
	}, nil
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
	if row, cerr := p.cache.Get(ctx, cacheKey); cerr == nil {
		if resp, ferr := p.fromCache(ctx, row, opts, req.Text); ferr == nil {
			return resp, nil
		}
	}
	return dto.TranslationResponse{}, mapProviderError(err)
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

func buildUserPrompt(kind, applied, schemaJSON string) string {
	var sb strings.Builder
	if kind == nlp.KindWord {
		sb.WriteString("请对下面的英文单词进行结构化词典查询，释义使用中文。\n\n")
	} else {
		sb.WriteString("请将下面的英文内容翻译为简体中文，保留 Markdown 结构与段落对应。\n\n")
	}
	sb.WriteString("<<<INPUT\n")
	sb.WriteString(applied)
	sb.WriteString("\nINPUT>>>\n\n")
	sb.WriteString("输出必须符合以下 JSON Schema：\n")
	sb.WriteString(schemaJSON)
	return sb.String()
}

func buildRepairPrompt(kind, invalidPayload, schemaJSON string) string {
	return "你上一次的输出不符合要求的 JSON Schema。无效输出：\n" +
		invalidPayload + "\n\n请修正，并仅输出一个严格符合以下 JSON Schema 的 JSON 对象，不要任何解释：\n" + schemaJSON
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
