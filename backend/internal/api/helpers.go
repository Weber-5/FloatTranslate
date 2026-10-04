package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// maxBodyBytes bounds every request body.
const maxBodyBytes = 8 << 20

// writeJSON writes v as a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// decodeJSON reads and decodes a JSON request body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return apperr.New(apperr.CodeInvalidRequest, "请求体过大或读取失败", false)
	}
	if len(body) == 0 {
		return apperr.New(apperr.CodeInvalidRequest, "请求体不能为空，且必须是 JSON", false)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return apperr.New(apperr.CodeInvalidRequest, "请求体不是合法 JSON", false)
	}
	return nil
}

// decodeJSONObject decodes a request body that must be a JSON object.
func decodeJSONObject(w http.ResponseWriter, r *http.Request, v any) error {
	if err := decodeJSON(w, r, v); err != nil {
		return err
	}
	return nil
}

// mapRepoError converts repository sentinel errors to standard app errors.
func mapRepoError(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return apperr.New(apperr.CodeNotFound, what+"不存在", false)
	case errors.Is(err, repository.ErrConflict):
		return apperr.New(apperr.CodeConflict, what+"已存在或冲突", false)
	default:
		return apperr.Wrap(apperr.CodeDatabaseError, "数据库操作失败", true, err)
	}
}
