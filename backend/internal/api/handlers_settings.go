package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

// settingsKeyProvider is the settings-table row holding the non-secret
// provider configuration.
const settingsKeyProvider = "provider"

// Provider modes (openapi enum); the wire protocol is shared.
const (
	ModeDeepseek         = llm.ModeDeepseek
	ModeOpenAICompatible = llm.ModeOpenAICompatible
)

// ProviderSettingsStore keeps the non-secret provider configuration in the
// settings table and the API key in memory only (Phase 2 — Credential
// Manager integration arrives in Phase 5; nothing secret is written to disk
// and a backend restart clears it: api_key_configured=false).
type ProviderSettingsStore struct {
	repo *repository.SettingsRepo

	mu     sync.RWMutex
	apiKey string
}

// NewProviderSettingsStore builds the store.
func NewProviderSettingsStore(repo *repository.SettingsRepo) *ProviderSettingsStore {
	return &ProviderSettingsStore{repo: repo}
}

// defaults returns the frozen provider defaults (deepseek preset).
func defaults() dto.ProviderSettingsView {
	return dto.ProviderSettingsView{
		Mode:             ModeDeepseek,
		BaseURL:          llm.DefaultDeepseekBaseURL,
		TranslationModel: translation.DefaultTranslationModel,
		ChatModel:        translation.DefaultTranslationModel,
	}
}

// View builds the current ProviderSettingsView (never returns the key itself).
func (s *ProviderSettingsStore) View(ctx context.Context) (dto.ProviderSettingsView, error) {
	view := defaults()
	if raw, err := s.repo.Get(ctx, settingsKeyProvider); err == nil {
		var stored map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(raw), &stored); jsonErr == nil {
			applyString(stored, "mode", &view.Mode)
			applyString(stored, "base_url", &view.BaseURL)
			applyString(stored, "translation_model", &view.TranslationModel)
			applyString(stored, "chat_model", &view.ChatModel)
		}
	} else if err != repository.ErrNotFound {
		return dto.ProviderSettingsView{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	view.APIKeyConfigured = s.apiKey != ""
	if s.apiKey != "" {
		prefix := s.apiKey
		if len(prefix) > 3 {
			prefix = prefix[:3]
		}
		view.APIKeyHint = prefix + "..."
	}
	return view, nil
}

// Update persists the non-secret part and keeps a provided API key in memory.
func (s *ProviderSettingsStore) Update(ctx context.Context, upd dto.ProviderSettingsUpdate) error {
	upd.Mode = strings.TrimSpace(upd.Mode)
	upd.BaseURL = strings.TrimSpace(upd.BaseURL)
	upd.TranslationModel = strings.TrimSpace(upd.TranslationModel)
	upd.ChatModel = strings.TrimSpace(upd.ChatModel)
	if upd.Mode != ModeDeepseek && upd.Mode != ModeOpenAICompatible {
		return apperr.New(apperr.CodeInvalidRequest, "mode 仅支持 deepseek 或 openai_compatible", false)
	}
	if upd.BaseURL == "" {
		return apperr.New(apperr.CodeInvalidRequest, "base_url 不能为空", false)
	}
	if upd.TranslationModel == "" || upd.ChatModel == "" {
		return apperr.New(apperr.CodeInvalidRequest, "translation_model 与 chat_model 不能为空", false)
	}
	payload := map[string]string{
		"mode":              upd.Mode,
		"base_url":          upd.BaseURL,
		"translation_model": upd.TranslationModel,
		"chat_model":        upd.ChatModel,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := s.repo.Put(ctx, settingsKeyProvider, string(raw)); err != nil {
		return err
	}
	if upd.APIKey != "" {
		s.mu.Lock()
		s.apiKey = upd.APIKey
		s.mu.Unlock()
	}
	return nil
}

// TranslationProvider implements llm.Resolver: when mode, base_url, models
// and the in-memory API key are all present it builds the real
// OpenAI-compatible adapter; otherwise it reports llm.ErrNotConfigured so
// the pipeline answers PROVIDER_NOT_CONFIGURED (no mock fallback).
func (s *ProviderSettingsStore) TranslationProvider(ctx context.Context) (llm.Provider, error) {
	view, err := s.View(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	key := s.apiKey
	s.mu.RUnlock()
	if strings.TrimSpace(key) == "" ||
		strings.TrimSpace(view.BaseURL) == "" ||
		strings.TrimSpace(view.TranslationModel) == "" ||
		strings.TrimSpace(view.ChatModel) == "" ||
		(view.Mode != ModeDeepseek && view.Mode != ModeOpenAICompatible) {
		return nil, llm.ErrNotConfigured
	}
	return llm.NewOpenAIAdapter(llm.AdapterConfig{
		Mode:             view.Mode,
		BaseURL:          view.BaseURL,
		APIKey:           key,
		TranslationModel: view.TranslationModel,
		ChatModel:        view.ChatModel,
	}), nil
}

// TestConfig resolves the effective provider configuration for a connection
// test: the current saved settings with any full provider settings from the
// request body overriding them; the key comes from the body when supplied,
// otherwise from memory.
func (s *ProviderSettingsStore) TestConfig(ctx context.Context, upd *dto.ProviderSettingsUpdate) (llm.TestConfig, error) {
	view, err := s.View(ctx)
	if err != nil {
		return llm.TestConfig{}, err
	}
	if upd != nil {
		if mode := strings.TrimSpace(upd.Mode); mode != "" {
			view.Mode = mode
		}
		if base := strings.TrimSpace(upd.BaseURL); base != "" {
			view.BaseURL = base
		}
		if model := strings.TrimSpace(upd.TranslationModel); model != "" {
			view.TranslationModel = model
		}
	}
	s.mu.RLock()
	key := s.apiKey
	s.mu.RUnlock()
	if upd != nil && upd.APIKey != "" {
		key = upd.APIKey
	}
	return llm.TestConfig{
		Mode:    view.Mode,
		BaseURL: view.BaseURL,
		APIKey:  key,
		Model:   view.TranslationModel,
	}, nil
}

// TranslationModel returns the configured translation model for the pipeline.
func (s *ProviderSettingsStore) TranslationModel() string {
	view, err := s.View(context.Background())
	if err != nil {
		return translation.DefaultTranslationModel
	}
	if strings.TrimSpace(view.TranslationModel) == "" {
		return translation.DefaultTranslationModel
	}
	return view.TranslationModel
}

// ChatModel returns the configured chat model for the chat generation
// service (same default as the translation model slot).
func (s *ProviderSettingsStore) ChatModel() string {
	view, err := s.View(context.Background())
	if err != nil {
		return translation.DefaultTranslationModel
	}
	if strings.TrimSpace(view.ChatModel) == "" {
		return translation.DefaultTranslationModel
	}
	return view.ChatModel
}

func applyString(stored map[string]json.RawMessage, key string, dst *string) {
	if raw, ok := stored[key]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil && strings.TrimSpace(v) != "" {
			*dst = v
		}
	}
}

// settingsDefaults are the frozen Phase 2 app setting defaults. GET
// /settings merges stored rows over these (missing keys keep defaults).
func settingsDefaults() map[string]any {
	return map[string]any{
		"theme":                      "system",
		"max_context_tokens":         1000000,
		"max_output_tokens":          8192,
		"auto_compact":               true,
		"compact_threshold":          0.8,
		"global_context":             "",
		"ai_system_prompt":           "", // empty = built-in default
		"custom_translation_prompt":  "",
		"always_on_top":              true,
		"auto_start":                 false,
		"hotkey_show_hide":           "Ctrl+Alt+Space",
		"hotkey_translate_selection": "Ctrl+Alt+Q",
		"proxy_mode":                 "system",
		"proxy_url":                  "",
	}
}

// mergedSettings loads stored settings rows over the defaults. The provider
// row is excluded (it is exposed through /settings/provider).
func (s *Server) mergedSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	merged := make(map[string]json.RawMessage, len(settingsDefaults())+8)
	for key, value := range settingsDefaults() {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		merged[key] = raw
	}
	rows, err := s.settings.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Key == settingsKeyProvider {
			continue
		}
		merged[row.Key] = json.RawMessage(row.ValueJSON)
	}
	return merged, nil
}

// GetSettings implements GET /api/v1/settings — non-secret settings merged
// over the frozen defaults.
func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	merged, err := s.mergedSettings(r.Context())
	if err != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "读取设置失败", true, err))
		return
	}
	writeJSON(w, http.StatusOK, merged)
}

// PutSettings implements PUT /api/v1/settings: the body is a flat JSON
// object whose top-level keys are stored row-per-key (arbitrary non-secret
// kv rows are accepted).
func (s *Server) PutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	for key, value := range body {
		if key == settingsKeyProvider {
			continue // provider settings go through /settings/provider
		}
		if err := s.settings.Put(r.Context(), key, string(value)); err != nil {
			apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "保存设置失败", true, err))
			return
		}
	}
	s.GetSettings(w, r)
}

// GetProviderSettings implements GET /api/v1/settings/provider.
func (s *Server) GetProviderSettings(w http.ResponseWriter, r *http.Request) {
	view, err := s.providerSettings.View(r.Context())
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "设置"))
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// PutProviderSettings implements PUT /api/v1/settings/provider. The API key
// (when present) is kept in memory only in Phase 2.
func (s *Server) PutProviderSettings(w http.ResponseWriter, r *http.Request) {
	var upd dto.ProviderSettingsUpdate
	if err := decodeJSON(w, r, &upd); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if err := s.providerSettings.Update(r.Context(), upd); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	view, err := s.providerSettings.View(r.Context())
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "设置"))
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// TestProviderConnection implements POST /api/v1/settings/provider/test.
// It uses the current saved provider config (a full provider settings body
// overrides it) and probes GET {base}/models, falling back to POST
// {base}/chat/completions with a 1-token ping. The response is always
// {ok, message}; the message never contains the key.
func (s *Server) TestProviderConnection(w http.ResponseWriter, r *http.Request) {
	var upd *dto.ProviderSettingsUpdate
	if r.ContentLength != 0 {
		var body dto.ProviderSettingsUpdate
		if err := decodeJSON(w, r, &body); err != nil {
			apperr.WriteHTTP(w, err)
			return
		}
		upd = &body
	}
	cfg, err := s.providerSettings.TestConfig(r.Context(), upd)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "设置"))
		return
	}
	res := llm.TestConnection(r.Context(), cfg)
	writeJSON(w, http.StatusOK, dto.TestConnectionResponse{OK: res.OK, Message: res.Message})
}
