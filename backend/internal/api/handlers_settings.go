package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

// settingsKeyProvider is the settings-table row holding the non-secret
// provider configuration.
const settingsKeyProvider = "provider"

// Provider modes (openapi enum).
const (
	ModeDeepseek         = "deepseek"
	ModeOpenAICompatible = "openai_compatible"
)

// ProviderSettingsStore keeps the non-secret provider configuration in the
// settings table and the API key in memory only (Phase 1 — Credential
// Manager integration arrives in Phase 5; nothing secret is written to disk).
type ProviderSettingsStore struct {
	repo *repository.SettingsRepo

	mu     sync.RWMutex
	apiKey string
}

// NewProviderSettingsStore builds the store.
func NewProviderSettingsStore(repo *repository.SettingsRepo) *ProviderSettingsStore {
	return &ProviderSettingsStore{repo: repo}
}

// defaults returns the frozen Phase 1 provider defaults.
func defaults() dto.ProviderSettingsView {
	return dto.ProviderSettingsView{
		Mode:             ModeDeepseek,
		BaseURL:          "https://api.deepseek.com",
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

func applyString(stored map[string]json.RawMessage, key string, dst *string) {
	if raw, ok := stored[key]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil && strings.TrimSpace(v) != "" {
			*dst = v
		}
	}
}

// GetSettings implements GET /api/v1/settings — the non-secret settings kv
// blob passed through as one JSON object.
func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.settings.List(r.Context())
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	out := map[string]json.RawMessage{}
	for _, row := range rows {
		out[row.Key] = json.RawMessage(row.ValueJSON)
	}
	writeJSON(w, http.StatusOK, out)
}

// PutSettings implements PUT /api/v1/settings: the body is a JSON object
// whose keys are stored row-per-key (full-object replace per key).
func (s *Server) PutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if err := decodeJSONObject(w, r, &body); err != nil {
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
// (when present) is kept in memory only in Phase 1.
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
// Phase 1 uses the mock provider and never echoes any key material.
func (s *Server) TestProviderConnection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, dto.TestConnectionResponse{
		OK:      true,
		Message: "测试连接成功（mock）",
	})
}
