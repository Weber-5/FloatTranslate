package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// ListVocabulary implements GET /api/v1/vocabulary?query= — items sorted by
// saved_at DESC, filtered case-insensitively on word/lemma/meanings.
func (s *Server) ListVocabulary(w http.ResponseWriter, r *http.Request) {
	rows, err := s.vocabulary.List(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "单词本"))
		return
	}
	items := make([]dto.VocabularyItem, 0, len(rows))
	for _, row := range rows {
		item, derr := decodeVocabularyRow(row)
		if derr != nil {
			apperr.WriteHTTP(w, derr)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

// SaveVocabulary implements PUT /api/v1/vocabulary/{lemma}: idempotent upsert
// of the full WordTranslation. An existing lemma (NOCASE) has its word_json
// and last_viewed_at updated; saved_at keeps the original first-save time.
// The lemma path value is URL-decoded by the router.
func (s *Server) SaveVocabulary(w http.ResponseWriter, r *http.Request) {
	lemma := chi.URLParam(r, "lemma")
	var word dto.WordTranslation
	if err := decodeJSON(w, r, &word); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if lemma == "" {
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "lemma 不能为空", false))
		return
	}
	if word.Word == "" {
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "word 不能为空", false))
		return
	}
	raw, merr := json.Marshal(word)
	if merr != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeInternal, "单词结果序列化失败", false, merr))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := repository.VocabularyRow{
		Lemma:        lemma,
		WordJSON:     string(raw),
		SavedAt:      now, // kept as the first-save time on conflict (repo semantics)
		LastViewedAt: now,
	}
	if err := s.vocabulary.Upsert(r.Context(), row); err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "单词本"))
		return
	}
	stored, err := s.vocabulary.Get(r.Context(), lemma)
	if err != nil {
		apperr.WriteHTTP(w, mapRepoError(err, "单词本"))
		return
	}
	item, derr := decodeVocabularyRow(stored)
	if derr != nil {
		apperr.WriteHTTP(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// DeleteVocabulary implements DELETE /api/v1/vocabulary/{lemma}. Deletion is
// idempotent: removing an unknown lemma still yields 204 (Phase 2 freeze).
func (s *Server) DeleteVocabulary(w http.ResponseWriter, r *http.Request) {
	lemma := chi.URLParam(r, "lemma")
	err := s.vocabulary.Delete(r.Context(), lemma)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		apperr.WriteHTTP(w, mapRepoError(err, "单词本"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeVocabularyRow builds the wire item for one stored row.
func decodeVocabularyRow(row repository.VocabularyRow) (dto.VocabularyItem, error) {
	var word dto.WordTranslation
	if err := json.Unmarshal([]byte(row.WordJSON), &word); err != nil {
		return dto.VocabularyItem{}, apperr.Wrap(apperr.CodeDatabaseError, "单词本数据损坏", false, err)
	}
	return dto.VocabularyItem{
		Lemma:        row.Lemma,
		Word:         &word,
		SavedAt:      row.SavedAt,
		LastViewedAt: row.LastViewedAt,
	}, nil
}
