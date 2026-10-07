package api

import (
	"context"
	"encoding/json"
	"errors"
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
	resp, err := s.translateWithBudget(r.Context(), req, translation.Options{})
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// translateWithBudget runs one translation under the server's end-to-end
// budget. The request context already cancels when the client disconnects
// (net/http); the deadline additionally guarantees that a chunked document
// cannot keep a request alive for tens of minutes when the provider stalls.
func (s *Server) translateWithBudget(ctx context.Context, req dto.TranslationRequest,
	opts translation.Options) (dto.TranslationResponse, error) {
	budget := s.translationBudget
	if budget <= 0 {
		budget = DefaultTranslationBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	resp, err := s.pipeline.Translate(ctx, req, opts)
	return resp, mapTranslationDeadline(ctx, err)
}

// mapTranslationDeadline turns "the request budget expired" into a clear,
// retryable failure instead of leaking a generic provider error (the frontend
// shows the message verbatim and offers a retry).
func mapTranslationDeadline(ctx context.Context, err error) error {
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	return apperr.New(apperr.CodeTranslationFailed,
		"翻译超时（超过 5 分钟），请缩短文本或稍后重试", true)
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
	resp, terr := s.translateWithBudget(r.Context(), req, translation.Options{HistoryID: row.ID})
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
