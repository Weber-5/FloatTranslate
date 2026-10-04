package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
)

// ListTerminology implements GET /api/v1/terminology.
func (s *Server) ListTerminology(w http.ResponseWriter, r *http.Request) {
	rows := s.terms.Snapshot()
	items := make([]dto.TerminologyItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, dto.TerminologyItem{ID: row.ID, Source: row.Source, Target: row.Target})
	}
	writeJSON(w, http.StatusOK, items)
}

// CreateTerminology implements POST /api/v1/terminology.
func (s *Server) CreateTerminology(w http.ResponseWriter, r *http.Request) {
	var body dto.TerminologyMutation
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if body.Source == "" || body.Target == "" {
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "source 与 target 不能为空", false))
		return
	}
	row, err := s.terms.Create(r.Context(), body.Source, body.Target)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "术语"))
		return
	}
	writeJSON(w, http.StatusCreated, dto.TerminologyItem{ID: row.ID, Source: row.Source, Target: row.Target})
}

// UpdateTerminology implements PUT /api/v1/terminology/{id}.
func (s *Server) UpdateTerminology(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body dto.TerminologyMutation
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if body.Source == "" || body.Target == "" {
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "source 与 target 不能为空", false))
		return
	}
	row, err := s.terms.Update(r.Context(), id, body.Source, body.Target)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "术语"))
		return
	}
	writeJSON(w, http.StatusOK, dto.TerminologyItem{ID: row.ID, Source: row.Source, Target: row.Target})
}

// DeleteTerminology implements DELETE /api/v1/terminology/{id}.
func (s *Server) DeleteTerminology(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.terms.Delete(r.Context(), id); err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "术语"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
