// Package logging provides the slog-based logger used by the backend plus the
// mandatory redaction layer (docs/08 §4): Authorization headers, Bearer
// tokens, api_key values, session tokens and any explicitly registered secret
// value must never reach a log sink; they are replaced with ***REDACTED***.
package logging

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// RedactedPlaceholder replaces every redacted value.
const RedactedPlaceholder = "***REDACTED***"

// sensitiveKeyTokens are lowercase key fragments that mark a structured-log
// attribute value as secret. A key matches when its normalized form equals or
// contains one of these tokens.
var sensitiveKeyTokens = []string{
	"authorization",
	"apikey",
	"authtoken",
	"accesstoken",
	"sessiontoken",
	"token",
	"secret",
	"password",
	"credential",
}

// bearerPattern matches "Bearer <token>" (any casing) so unregistered bearer
// credentials are still redacted as defense in depth.
var bearerPattern = regexp.MustCompile(`(?i)\b(bearer[\s:=]+)[A-Za-z0-9._~+/=-]{4,}`)

// kvPattern matches `"<sensitive key>" : "<value>"` or `sensitive_key=value`
// shapes for authorization / api_key style keys inside free-form strings.
var kvPattern = regexp.MustCompile(`(?i)("?(?:authorization|api[_-]?key|session[_-]?token)"?"?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}"]+)`)

// Redactor holds registered secret values and redacts them from strings and
// structured log attributes. It is safe for concurrent use.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

// NewRedactor returns an empty Redactor.
func NewRedactor() *Redactor { return &Redactor{} }

// Register adds a literal secret value that must never appear in logs.
// Empty or very short values are ignored (they carry no secret value and
// would over-redact).
func (r *Redactor) Register(secret string) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 4 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.secrets {
		if s == secret {
			return
		}
	}
	r.secrets = append(r.secrets, secret)
}

// Redact removes every registered secret, bearer token and sensitive
// key=value pair from s, replacing them with RedactedPlaceholder.
func (r *Redactor) Redact(s string) string {
	if s == "" {
		return s
	}
	r.mu.RLock()
	secrets := r.secrets
	r.mu.RUnlock()
	// Longest first so overlapping secrets are fully removed.
	for i := 1; i < len(secrets); i++ {
		for j := i; j > 0 && len(secrets[j]) > len(secrets[j-1]); j-- {
			secrets[j], secrets[j-1] = secrets[j-1], secrets[j]
		}
	}
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, RedactedPlaceholder)
		}
	}
	s = bearerPattern.ReplaceAllString(s, "${1}"+RedactedPlaceholder)
	s = kvPattern.ReplaceAllString(s, "${1}"+RedactedPlaceholder)
	return s
}

func isSensitiveKey(key string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	normalized := b.String()
	if normalized == "" {
		return false
	}
	for _, tok := range sensitiveKeyTokens {
		if strings.Contains(normalized, tok) {
			return true
		}
	}
	return false
}

func (r *Redactor) redactAttr(a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, RedactedPlaceholder)
	}
	switch v := a.Value.Any().(type) {
	case string:
		return slog.String(a.Key, r.Redact(v))
	case []slog.Attr:
		out := make([]slog.Attr, len(v))
		for i, inner := range v {
			out[i] = r.redactAttr(inner)
		}
		return slog.Any(a.Key, out)
	case slog.Value:
		return slog.Any(a.Key, r.redactValue(v))
	default:
		return a
	}
}

func (r *Redactor) redactValue(v slog.Value) slog.Value {
	switch v.Kind() {
	case slog.KindString:
		return slog.StringValue(r.Redact(v.String()))
	case slog.KindGroup:
		attrs := v.Group()
		out := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			out[i] = r.redactAttr(a)
		}
		return slog.GroupValue(out...)
	default:
		return v
	}
}

// redactHandler is a slog.Handler decorator that redacts the message and all
// attributes (including pre-formatted ones) before delegating.
type redactHandler struct {
	inner    slog.Handler
	redactor *Redactor
	with     []slog.Attr
	group    string
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	msg := h.redactor.Redact(rec.Message)
	out := slog.NewRecord(rec.Time, rec.Level, msg, rec.PC)
	for _, a := range h.with {
		out.AddAttrs(h.redactor.redactAttr(a))
	}
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redactor.redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.with)+len(attrs))
	merged = append(merged, h.with...)
	merged = append(merged, attrs...)
	return &redactHandler{inner: h.inner, redactor: h.redactor, with: merged, group: h.group}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &redactHandler{inner: h.inner.WithGroup(name), redactor: h.redactor, with: h.with, group: name}
}

// New builds a JSON logger writing to w whose messages and attribute values
// pass through redactor. The returned logger never emits secret values.
func New(w io.Writer, level slog.Level, redactor *Redactor) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	var h slog.Handler = handler
	if redactor == nil {
		redactor = NewRedactor()
	}
	h = &redactHandler{inner: handler, redactor: redactor}
	return slog.New(h)
}

// LogRequest writes the standard request line: method, path, status and
// duration only — never headers, query strings or bodies.
func LogRequest(logger *slog.Logger, method, path string, status int, durationMS float64) {
	logger.LogAttrs(context.Background(), slog.LevelInfo, "http_request",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", status),
		slog.Float64("duration_ms", durationMS),
	)
}
