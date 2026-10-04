package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

// CreateTranslation implements POST /api/v1/translations.
func (s *Server) CreateTranslation(w http.ResponseWriter, r *http.Request) {
	var req dto.TranslationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	resp, err := s.pipeline.Translate(r.Context(), req, translation.Options{})
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetTranslation implements GET /api/v1/translations/{id}.
func (s *Server) GetTranslation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := s.history.Get(r.Context(), id)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "翻译记录"))
		return
	}
	result, derr := decodeResult(row.ResultJSON)
	if derr != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "历史结果损坏", false, derr))
		return
	}
	writeJSON(w, http.StatusOK, dto.TranslationResponse{
		TranslationID: row.ID,
		Kind:          row.Kind,
		Source:        row.Source,
		Result:        result,
		Model:         row.Model,
		CreatedAt:     row.CreatedAt,
	})
}

// Retranslate implements POST /api/v1/translations/{id}/retranslate: reruns
// the pipeline with bypass_cache=true and updates the current history entry
// in place (docs/04 §5).
func (s *Server) Retranslate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := s.history.Get(r.Context(), id)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "翻译记录"))
		return
	}
	req := dto.TranslationRequest{
		Text:        row.InputText,
		ForceKind:   row.Kind,
		BypassCache: true,
	}
	resp, terr := s.pipeline.Translate(r.Context(), req, translation.Options{HistoryID: row.ID})
	if terr != nil {
		apperr.WriteHTTP(w, terr)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// decodeResult unmarshals a stored schema-validated result JSON.
func decodeResult(raw string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	return v, nil
}
