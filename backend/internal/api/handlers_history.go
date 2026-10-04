package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// ListHistory implements GET /api/v1/history with query/kind/limit/cursor.
func (s *Server) ListHistory(w http.ResponseWriter, r *http.Request) {
	params := repository.ListParams{
		Query: r.URL.Query().Get("query"),
		Kind:  r.URL.Query().Get("kind"),
	}
	switch params.Kind {
	case "", "word", "text":
	default:
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "kind 仅支持 word 或 text", false))
		return
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "limit 必须是 1-200 的整数", false))
			return
		}
		params.Limit = limit
	}
	params.Cursor = r.URL.Query().Get("cursor")

	rows, next, err := s.history.List(r.Context(), params)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "历史记录"))
		return
	}
	items := make([]dto.HistoryItem, 0, len(rows))
	for _, row := range rows {
		result, derr := decodeResult(row.ResultJSON)
		if derr != nil {
			apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "历史结果损坏", false, derr))
			return
		}
		items = append(items, dto.HistoryItem{
			ID:        row.ID,
			Kind:      row.Kind,
			InputText: row.InputText,
			Result:    result,
			CreatedAt: row.CreatedAt,
		})
	}
	page := dto.HistoryPage{Items: items}
	if next != "" {
		page.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, page)
}

// ClearHistory implements DELETE /api/v1/history.
func (s *Server) ClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.history.DeleteAll(r.Context()); err != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "清空历史失败", true, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteHistoryItem implements DELETE /api/v1/history/{id}. Vocabulary is
// never touched (docs/04 §6).
func (s *Server) DeleteHistoryItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.history.Delete(r.Context(), id); err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "历史记录"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
